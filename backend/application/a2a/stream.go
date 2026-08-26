package a2aapp

import (
	"context"
	"errors"
	"iter"
	"sync"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// EventSubscriber delivers live per-case events. The server EventBroker
// implements this structurally without the application layer importing it.
type EventSubscriber interface {
	Subscribe(caseID string) chan *entity.MagiEvent
	Unsubscribe(caseID string, ch chan *entity.MagiEvent)
}

// DurableStreamProjector merges the durable snapshot, the local broker, and the
// database poller into one ordered A2A event stream per task.
type DurableStreamProjector struct {
	repo              SubmissionRepository
	events            port.EventRepository
	broker            EventSubscriber
	projector         *TaskProjector
	maxStreamsPerUser int
	pollInterval      time.Duration
	active            map[int64]int
	mu                sync.Mutex
	metrics           *metrics.Registry
}

// StreamOption configures optional observability on the stream projector.
type StreamOption func(*DurableStreamProjector)

// WithStreamMetrics enables the active-stream gauge and projection errors.
func WithStreamMetrics(reg *metrics.Registry) StreamOption {
	return func(s *DurableStreamProjector) { s.metrics = reg }
}

func NewDurableStreamProjector(repo SubmissionRepository, events port.EventRepository, broker EventSubscriber, projector *TaskProjector, maxStreamsPerUser int, pollInterval time.Duration, opts ...StreamOption) *DurableStreamProjector {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	s := &DurableStreamProjector{
		repo: repo, events: events, broker: broker, projector: projector,
		maxStreamsPerUser: maxStreamsPerUser, pollInterval: pollInterval,
		active: make(map[int64]int),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Events returns the ordered A2A event iterator for an owner-scoped task.
func (s *DurableStreamProjector) Events(ctx context.Context, userID int64, taskID string) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		s.run(ctx, userID, taskID, yield)
	}
}

func (s *DurableStreamProjector) run(ctx context.Context, userID int64, taskID string, yield func(a2a.Event, error) bool) {
	start := time.Now()
	ctx, span := tracing.Start(ctx, "a2a.stream")
	defer span.End()
	defer func() {
		if s.metrics != nil {
			s.metrics.RecordA2AStreamDuration(time.Since(start).Milliseconds())
		}
	}()

	release := s.acquire(userID)
	if release == nil {
		yield(nil, ErrStreamLimitExceeded)
		return
	}
	defer release()

	record, err := s.repo.GetTaskRecord(ctx, userID, taskID)
	if errors.Is(err, ErrNotFound) {
		yield(nil, a2a.NewError(a2a.ErrTaskNotFound, "task not found"))
		return
	}
	if err != nil {
		s.yieldInternalError(ctx, yield, "initial task read", err)
		return
	}
	initial := s.projector.Project(record, 1, true)
	if initial.Status.State.Terminal() {
		yield(initial, nil)
		return
	}
	if !yield(initial, nil) {
		return
	}

	state := &streamState{
		task:       initial,
		watermark:  record.MaxEventSeq,
		lastState:  initial.Status.State,
		lastPaused: pausedOf(initial),
	}
	ch := s.broker.Subscribe(taskID)
	defer s.broker.Unsubscribe(taskID, ch)

	// Durable catch-up closes the window between the snapshot watermark and
	// the broker subscription.
	finished, err := s.drainEvents(ctx, userID, taskID, state, yield)
	if err != nil {
		s.yieldInternalError(ctx, yield, "durable catch-up", err)
		return
	}
	if finished {
		return
	}

	// Live merge: local broker events, the DB poll for remote instances, and
	// consumer cancellation.
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			if ev == nil {
				continue
			}
			// Broker events are wake-ups. A live event that has not yet been
			// persisted is out of order; drain the durable store first so the
			// sequence is never skipped. Only applyOrderedEvent advances the
			// watermark.
			if ev.Seq > state.watermark+1 {
				finished, err := s.drainEvents(ctx, userID, taskID, state, yield)
				if err != nil {
					s.yieldInternalError(ctx, yield, "gap drain", err)
					return
				}
				if finished {
					return
				}
			}
			if ev.Seq <= state.watermark {
				// The durable drain already replayed this event as the ordered
				// copy; discard the live duplicate.
				continue
			}
			if ev.Seq != state.watermark+1 {
				// The durable store still has a gap (the live fan-out raced
				// ahead of the durable write). Retain the watermark and wait
				// for the poller to fill the missing event in order.
				continue
			}
			done, err := s.handleEvent(ctx, userID, taskID, state, ev, yield)
			if err != nil {
				s.yieldInternalError(ctx, yield, "live event", err)
				return
			}
			if done {
				return
			}
		case <-ticker.C:
			finished, err := s.drainEvents(ctx, userID, taskID, state, yield)
			if err != nil {
				s.yieldInternalError(ctx, yield, "durable poll", err)
				return
			}
			if finished {
				return
			}
		}
	}
}

// drainEvents replays every durable event after the watermark in order. It
// returns (true, nil) when a terminal event ended the stream and (false, nil)
// when it is caught up without a terminal state.
func (s *DurableStreamProjector) drainEvents(ctx context.Context, userID int64, taskID string, state *streamState, yield func(a2a.Event, error) bool) (bool, error) {
	for {
		batch, err := s.events.ListAfterSeq(ctx, taskID, state.watermark, 100)
		if err != nil {
			return false, err
		}
		if len(batch) == 0 {
			// The Case/Job snapshot is authoritative. This closes streams for
			// legacy rows where a terminal status committed without its event.
			record, err := s.repo.GetTaskRecord(ctx, userID, taskID)
			if err != nil {
				return false, err
			}
			task := s.projector.Project(record, 1, true)
			// A completed Case can briefly precede its Resolution in legacy
			// split-writer rows. Wait for the artifact authority before closing;
			// failure/cancellation snapshots do not require artifacts.
			completedReady := task.Status.State != a2a.TaskStateCompleted || record.Resolution != nil
			if task.Status.State.Terminal() && completedReady && !state.lastState.Terminal() {
				state.task = task
				state.lastState = task.Status.State
				state.lastPaused = pausedOf(task)
				return s.emitTerminal(task, yield), nil
			}
			return false, nil
		}
		for _, ev := range batch {
			done, err := s.handleEvent(ctx, userID, taskID, state, ev, yield)
			if err != nil {
				return false, err
			}
			if done {
				return true, nil
			}
		}
	}
}

// handleEvent advances the watermark unconditionally, filters non-public
// events, then emits status/artifact updates for visible FSM changes.
func (s *DurableStreamProjector) handleEvent(ctx context.Context, userID int64, taskID string, state *streamState, ev *entity.MagiEvent, yield func(a2a.Event, error) bool) (bool, error) {
	if ev == nil || ev.Seq <= state.watermark {
		return false, nil
	}
	if s.metrics != nil {
		s.metrics.RecordA2AEventLag(time.Since(ev.Timestamp).Milliseconds())
	}
	state.watermark = ev.Seq
	if !isPublicStatusEvent(ev.Type) {
		return false, nil
	}
	record, err := s.repo.GetTaskRecord(ctx, userID, taskID)
	if err != nil {
		return false, err
	}
	task := s.projector.Project(record, 1, true)
	state.task = task
	if task.Status.State == state.lastState && pausedOf(task) == state.lastPaused {
		return false, nil
	}
	state.lastState = task.Status.State
	state.lastPaused = pausedOf(task)
	if task.Status.State.Terminal() {
		return s.emitTerminal(task, yield), nil
	}
	if !yield(&a2a.TaskStatusUpdateEvent{
		ContextID: task.ContextID, TaskID: task.ID, Status: task.Status,
	}, nil) {
		return true, nil
	}
	return false, nil
}

func (s *DurableStreamProjector) emitTerminal(task *a2a.Task, yield func(a2a.Event, error) bool) bool {
	if task.Status.State == a2a.TaskStateCompleted {
		for _, artifact := range task.Artifacts {
			if !yield(&a2a.TaskArtifactUpdateEvent{
				Append: false, LastChunk: true, Artifact: artifact,
				ContextID: task.ContextID, TaskID: task.ID,
			}, nil) {
				return true
			}
		}
	}
	if !yield(&a2a.TaskStatusUpdateEvent{
		ContextID: task.ContextID, TaskID: task.ID, Status: task.Status,
	}, nil) {
		return true
	}
	return true
}

// yieldInternalError records the original error under the active tracing span
// but only exposes a stable public error to the client, so SQL, paths, tool
// responses, and other internal text never leak over the protocol.
func (s *DurableStreamProjector) yieldInternalError(ctx context.Context, yield func(a2a.Event, error) bool, op string, err error) {
	if s.metrics != nil {
		s.metrics.IncA2AProjectionError(metrics.A2AProjectionStatus)
	}
	if span := trace.SpanFromContext(ctx); span != nil && span.SpanContext().IsValid() {
		span.RecordError(err)
		span.SetStatus(codes.Error, op)
	}
	yield(nil, a2a.NewError(a2a.ErrInternalError, "internal task stream error"))
}

func (s *DurableStreamProjector) acquire(userID int64) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxStreamsPerUser > 0 && s.active[userID] >= s.maxStreamsPerUser {
		return nil
	}
	s.active[userID]++
	if s.metrics != nil {
		s.metrics.A2AStreamStart()
	}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active[userID]--
		if s.metrics != nil {
			s.metrics.A2AStreamEnd()
		}
		if s.active[userID] == 0 {
			delete(s.active, userID)
		}
	}
}

func isPublicStatusEvent(t entity.EventType) bool {
	switch t {
	case entity.EventCaseStatusChanged, entity.EventCaseCompleted, entity.EventCaseFailed,
		entity.EventToolApprovalRequested, entity.EventToolApprovalResolved:
		return true
	default:
		return false
	}
}

func pausedOf(task *a2a.Task) bool {
	if task == nil || task.Metadata == nil {
		return false
	}
	if magi, ok := task.Metadata["magi"].(map[string]any); ok {
		if paused, ok := magi["paused"].(bool); ok {
			return paused
		}
	}
	return false
}

type streamState struct {
	task       *a2a.Task
	watermark  uint64
	lastState  a2a.TaskState
	lastPaused bool
}

var _ StreamProjector = (*DurableStreamProjector)(nil)
