package a2aapp

import (
	"context"
	"errors"
	"iter"
	"sync"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
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
}

func NewDurableStreamProjector(repo SubmissionRepository, events port.EventRepository, broker EventSubscriber, projector *TaskProjector, maxStreamsPerUser int, pollInterval time.Duration) *DurableStreamProjector {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &DurableStreamProjector{
		repo: repo, events: events, broker: broker, projector: projector,
		maxStreamsPerUser: maxStreamsPerUser, pollInterval: pollInterval,
		active: make(map[int64]int),
	}
}

// Events returns the ordered A2A event iterator for an owner-scoped task.
func (s *DurableStreamProjector) Events(ctx context.Context, userID int64, taskID string) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		s.run(ctx, userID, taskID, yield)
	}
}

func (s *DurableStreamProjector) run(ctx context.Context, userID int64, taskID string, yield func(a2a.Event, error) bool) {
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
		yield(nil, err)
		return
	}
	initial := s.projector.Project(record, 1)
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
	if err := s.drainEvents(ctx, userID, taskID, state, yield); err != nil {
		yield(nil, err)
		return
	}
	if s.finished(state) {
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
			done, err := s.handleEvent(ctx, userID, taskID, state, ev, yield)
			if err != nil {
				yield(nil, err)
				return
			}
			if done {
				return
			}
		case <-ticker.C:
			if err := s.drainEvents(ctx, userID, taskID, state, yield); err != nil {
				yield(nil, err)
				return
			}
			if s.finished(state) {
				return
			}
		}
	}
}

// drainEvents replays every durable event after the watermark. It returns when
// caught up or when a terminal event ended the stream.
func (s *DurableStreamProjector) drainEvents(ctx context.Context, userID int64, taskID string, state *streamState, yield func(a2a.Event, error) bool) error {
	for {
		batch, err := s.events.ListAfterSeq(ctx, taskID, state.watermark, 100)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, ev := range batch {
			done, err := s.handleEvent(ctx, userID, taskID, state, ev, yield)
			if err != nil {
				return err
			}
			if done {
				return nil
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
	state.watermark = ev.Seq
	if !isPublicStatusEvent(ev.Type) {
		return false, nil
	}
	record, err := s.repo.GetTaskRecord(ctx, userID, taskID)
	if err != nil {
		return false, err
	}
	task := s.projector.Project(record, 1)
	if task.Status.State == state.lastState && pausedOf(task) == state.lastPaused {
		return false, nil
	}
	state.lastState = task.Status.State
	state.lastPaused = pausedOf(task)
	if task.Status.State.Terminal() {
		if task.Status.State == a2a.TaskStateCompleted {
			for _, artifact := range task.Artifacts {
				if !yield(&a2a.TaskArtifactUpdateEvent{
					Append: false, LastChunk: true, Artifact: artifact,
					ContextID: task.ContextID, TaskID: task.ID,
				}, nil) {
					return true, nil
				}
			}
		}
		if !yield(&a2a.TaskStatusUpdateEvent{
			ContextID: task.ContextID, TaskID: task.ID, Status: task.Status,
		}, nil) {
			return true, nil
		}
		return true, nil
	}
	if !yield(&a2a.TaskStatusUpdateEvent{
		ContextID: task.ContextID, TaskID: task.ID, Status: task.Status,
	}, nil) {
		return true, nil
	}
	return false, nil
}

func (s *DurableStreamProjector) finished(state *streamState) bool {
	return state.task != nil && state.task.Status.State.Terminal()
}

func (s *DurableStreamProjector) acquire(userID int64) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxStreamsPerUser > 0 && s.active[userID] >= s.maxStreamsPerUser {
		return nil
	}
	s.active[userID]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active[userID]--
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
