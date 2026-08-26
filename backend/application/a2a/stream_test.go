package a2aapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
)

// testEventBroker is a deterministic broker + event repository for the stream
// tests. PublishRemote writes only to the durable store (a remote instance).
type testEventBroker struct {
	mu             sync.Mutex
	subs           map[string][]chan *entity.MagiEvent
	stored         map[string][]*entity.MagiEvent
	nextSeq        uint64
	blockSubscribe chan struct{}
}

func newTestEventBroker() *testEventBroker {
	return &testEventBroker{subs: make(map[string][]chan *entity.MagiEvent), stored: make(map[string][]*entity.MagiEvent)}
}

func (b *testEventBroker) Subscribe(caseID string) chan *entity.MagiEvent {
	if b.blockSubscribe != nil {
		<-b.blockSubscribe
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan *entity.MagiEvent, 64)
	b.subs[caseID] = append(b.subs[caseID], ch)
	return ch
}

func (b *testEventBroker) Unsubscribe(caseID string, ch chan *entity.MagiEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[caseID]
	for i, s := range subs {
		if s == ch {
			close(s)
			b.subs[caseID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (b *testEventBroker) Publish(ctx context.Context, e entity.MagiEvent) error {
	b.mu.Lock()
	b.nextSeq++
	e.Seq = b.nextSeq
	b.stored[e.CaseID] = append(b.stored[e.CaseID], &e)
	subs := append([]chan *entity.MagiEvent(nil), b.subs[e.CaseID]...)
	b.mu.Unlock()
	for _, ch := range subs {
		ch <- &e
	}
	return nil
}

func (b *testEventBroker) Create(ctx context.Context, e *entity.MagiEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSeq++
	e.Seq = b.nextSeq
	b.stored[e.CaseID] = append(b.stored[e.CaseID], e)
	return nil
}

func (b *testEventBroker) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*entity.MagiEvent(nil), b.stored[caseID]...), nil
}

func (b *testEventBroker) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*entity.MagiEvent
	for _, e := range b.stored[caseID] {
		if !e.Timestamp.Before(after) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (b *testEventBroker) PublishRemote(ctx context.Context, e entity.MagiEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSeq++
	e.Seq = b.nextSeq
	b.stored[e.CaseID] = append(b.stored[e.CaseID], &e)
	return nil
}

func (b *testEventBroker) ListAfterSeq(ctx context.Context, caseID string, afterSeq uint64, limit int) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var out []*entity.MagiEvent
	for _, e := range b.stored[caseID] {
		if e.Seq > afterSeq {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func newStreamHarness(t *testing.T, maxStreams int) (*a2aapp.DurableStreamProjector, *gorm.DB, a2aapp.SubmissionRepository, *testEventBroker) {
	t.Helper()
	db := openSubmissionDB(t)
	repo := magi.NewA2ASubmissionRepository(db)
	broker := newTestEventBroker()
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	stream := a2aapp.NewDurableStreamProjector(repo, broker, broker, proj, maxStreams, 10*time.Millisecond)
	return stream, db, repo, broker
}

func seedStreamTask(t *testing.T, db *gorm.DB, repo a2aapp.SubmissionRepository, taskID, contextID string, caseStatus entity.CaseStatus, jobStatus *entity.DecisionJobStatus) {
	t.Helper()
	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-" + taskID, MessageID: "msg-" + taskID, RequestHash: "hash",
		TaskID: taskID, ContextID: contextID, InputMessageID: "input-" + taskID,
		CaseMessageID: "case-msg-" + taskID, UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	token := "seed-" + taskID
	if _, claimed, err := repo.ClaimStart(context.Background(), "sub-"+taskID, token, time.Now().Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("claim seed binding = claimed %v err %v", claimed, err)
	}
	if err := repo.SettleStarted(context.Background(), "sub-"+taskID, token); err != nil {
		t.Fatal(err)
	}
	if jobStatus != nil {
		if err := db.Create(&magi.DecisionJobModel{ID: "job-" + taskID, CaseID: taskID, Status: string(*jobStatus), AvailableAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", taskID).Update("status", string(caseStatus)).Error; err != nil {
		t.Fatal(err)
	}
}

func running() *entity.DecisionJobStatus {
	s := entity.DecisionJobRunning
	return &s
}

func succeeded() *entity.DecisionJobStatus {
	s := entity.DecisionJobSucceeded
	return &s
}

func paused() *entity.DecisionJobStatus {
	s := entity.DecisionJobPaused
	return &s
}

func seedResolution(t *testing.T, db *gorm.DB, caseID string) {
	t.Helper()
	consensus, err := json.Marshal(entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.ResolutionModel{ID: "res-" + caseID, CaseID: caseID, FinalDecision: string(entity.VoteDecisionApprove), ConsensusJSON: string(consensus)}).Error; err != nil {
		t.Fatal(err)
	}
}

type streamCollector struct {
	mu     sync.Mutex
	events []a2a.Event
	done   chan struct{}
}

func startCollect(ctx context.Context, seq iter.Seq2[a2a.Event, error]) *streamCollector {
	c := &streamCollector{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for ev, err := range seq {
			if err != nil {
				return
			}
			c.mu.Lock()
			c.events = append(c.events, ev)
			c.mu.Unlock()
		}
	}()
	return c
}

func (c *streamCollector) snapshot() []a2a.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]a2a.Event(nil), c.events...)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestStreamProjector_EmitsFullCompletedSequence(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })
	got := c.snapshot()
	if _, ok := got[0].(*a2a.Task); !ok || got[0].TaskInfo().TaskID != "case-1" {
		t.Fatalf("initial event = %#v", got[0])
	}

	// Paused status change -> visible working/paused status update.
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusPaused)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", "case-1").Update("status", string(entity.DecisionJobPaused)).Error; err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusPaused)})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 2 })

	// Complete: update DB authority, then publish the terminal event.
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	seedResolution(t, db, "case-1")
	if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", "case-1").Update("status", string(entity.DecisionJobSucceeded)).Error; err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not terminate after completion")
	}

	events := c.snapshot()
	if len(events) < 4 {
		t.Fatalf("sequence = %d events", len(events))
	}
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Fatalf("first event not a Task: %#v", events[0])
	}
	if status, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok || status.Status.State != a2a.TaskStateWorking {
		t.Fatalf("second event = %#v", events[1])
	}
	md, ok1 := events[2].(*a2a.TaskArtifactUpdateEvent)
	js, ok2 := events[3].(*a2a.TaskArtifactUpdateEvent)
	if !ok1 || !ok2 || md.Artifact.ID != "case-1-decision-report" || js.Artifact.ID != "case-1-decision-result" {
		t.Fatalf("artifacts = %#v %#v", events[2], events[3])
	}
	if md.Append || !md.LastChunk || js.Append || !js.LastChunk {
		t.Fatalf("artifact chunk flags: %+v %+v", md, js)
	}
	final, ok := events[4].(*a2a.TaskStatusUpdateEvent)
	if !ok || final.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("final event = %#v", events[4])
	}
}

func TestStreamProjector_SubscribeTerminalClosesImmediately(t *testing.T) {
	stream, db, repo, _ := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusResolved, succeeded())
	seedResolution(t, db, "case-1")

	events := collectAll(t, context.Background(), stream.Events(context.Background(), 7, "case-1"))
	if len(events) != 1 {
		t.Fatalf("terminal subscribe events = %d, want 1", len(events))
	}
	task, ok := events[0].(*a2a.Task)
	if !ok || task.Status.State != a2a.TaskStateCompleted || len(task.Artifacts) != 2 {
		t.Fatalf("terminal snapshot = %#v", events[0])
	}
}

func TestStreamProjector_CanceledNeverEmitsResolutionArtifact(t *testing.T) {
	stream, db, repo, _ := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusCancelled, nil)
	seedResolution(t, db, "case-1") // late artifact must never leak

	events := collectAll(t, context.Background(), stream.Events(context.Background(), 7, "case-1"))
	if len(events) != 1 {
		t.Fatalf("canceled subscribe events = %d, want 1", len(events))
	}
	task, ok := events[0].(*a2a.Task)
	if !ok || task.Status.State != a2a.TaskStateCanceled || len(task.Artifacts) != 0 {
		t.Fatalf("canceled snapshot = %#v", events[0])
	}
}

func TestStreamProjector_EventPersistedBeforeSubscribeIsCaught(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())
	broker.blockSubscribe = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	// The stream is now blocked in Subscribe (snapshot already loaded).
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusPaused)).Error; err != nil {
		t.Fatal(err)
	}
	if err := broker.PublishRemote(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusPaused)})); err != nil {
		t.Fatal(err)
	}
	close(broker.blockSubscribe)

	waitFor(t, time.Second, func() bool {
		for _, ev := range c.snapshot() {
			if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
				return s.Status.State == a2a.TaskStateWorking
			}
		}
		return false
	})
	cancel()
	<-c.done
}

func TestStreamProjector_DuplicateBrokerAndDBEventsEmitOnce(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusPaused)).Error; err != nil {
		t.Fatal(err)
	}
	// The same durable event is both fanned out and re-read from the store.
	if err := broker.Publish(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusPaused)})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		count := 0
		for _, ev := range c.snapshot() {
			if _, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
				count++
			}
		}
		return count >= 1
	})
	time.Sleep(100 * time.Millisecond) // give the poller a chance to double-emit
	count := 0
	for _, ev := range c.snapshot() {
		if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok && s.Status.State == a2a.TaskStateWorking {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("working status updates = %d, want 1", count)
	}
	cancel()
	<-c.done
}

func TestStreamProjector_RemoteEventAppearsViaPolling(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusPaused)).Error; err != nil {
		t.Fatal(err)
	}
	// A remote replica writes the event to the shared store only.
	if err := broker.PublishRemote(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusPaused)})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		for _, ev := range c.snapshot() {
			if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
				return s.Status.State == a2a.TaskStateWorking
			}
		}
		return false
	})
	cancel()
	<-c.done
}

// TestStreamProjector_CatchUpTerminalClosesStream guards the catch-up path: a
// terminal event that lands while the stream is between the snapshot and its
// broker subscription must terminate the iterator. The stream must close
// itself after emitting the final status, not keep polling forever.
func TestStreamProjector_CatchUpTerminalClosesStream(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())
	broker.blockSubscribe = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	// A remote replica completes the case while the stream is blocked in
	// Subscribe (snapshot already loaded, terminal event only durable).
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	seedResolution(t, db, "case-1")
	if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", "case-1").Update("status", string(entity.DecisionJobSucceeded)).Error; err != nil {
		t.Fatal(err)
	}
	if err := broker.PublishRemote(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)})); err != nil {
		t.Fatal(err)
	}
	close(broker.blockSubscribe)

	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not terminate after the catch-up terminal event")
	}
	terminal := false
	for _, ev := range c.snapshot() {
		if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok && s.Status.State == a2a.TaskStateCompleted {
			terminal = true
		}
	}
	if !terminal {
		t.Fatalf("catch-up stream never emitted the terminal status: %#v", c.snapshot())
	}
}

// TestStreamProjector_LiveGapDoesNotSkipDurableEvent guards watermark
// integrity: when a live broker event with a sequence gap arrives while the
// durable event has not been drained, the stream must treat the live event as
// a wake-up and drain the durable store first. Here the durable N+1 is the
// terminal completion and the live N+2 is a non-public event; without the gap
// guard the watermark jumps to N+2 and the terminal event is missed forever.
func TestStreamProjector_LiveGapDoesNotSkipDurableEvent(t *testing.T) {
	_, db, repo, broker := newStreamHarness(t, 8)
	// A long poll interval keeps the live broker path deterministic: without
	// the gap guard the durable N+1 would never be drained before N+2 lands.
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	stream := a2aapp.NewDurableStreamProjector(repo, broker, broker, proj, 8, time.Hour)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	// Durable N+1 from a remote replica: the terminal completion.
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	seedResolution(t, db, "case-1")
	if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", "case-1").Update("status", string(entity.DecisionJobSucceeded)).Error; err != nil {
		t.Fatal(err)
	}
	if err := broker.PublishRemote(ctx, entity.NewEvent("case-1", "", nil, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)})); err != nil {
		t.Fatal(err)
	}

	// Live N+2 (non-public) arrives before N+1 was drained.
	if err := broker.Publish(ctx, entity.NewEvent("case-1", "", nil, entity.EventTaskNormalized, map[string]any{})); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream skipped the durable terminal event and never terminated")
	}
	terminal := false
	for _, ev := range c.snapshot() {
		if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok && s.Status.State == a2a.TaskStateCompleted {
			terminal = true
		}
	}
	if !terminal {
		t.Fatalf("live-gap stream never emitted the durable terminal status: %#v", c.snapshot())
	}
}

// TestStreamProjector_RemoteCancelClosesStream guards cross-replica
// cancellation: a CancelTask issued on another replica must surface to an
// established Subscribe as a canceled status and terminate the stream. A
// cancel that only mutates Case/Job without a durable event is invisible.
func TestStreamProjector_RemoteCancelClosesStream(t *testing.T) {
	stream, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })

	// CancelTask mutates the DB state and returns the durable CANCELLED event
	// it committed. In this harness the stream's durable journal is the
	// testEventBroker (not the SQLite magi_event table), so mirror the event
	// into the journal exactly as a remote replica's commit would become
	// visible to the shared store.
	cancelResult, err := repo.CancelTask(ctx, 7, "case-1")
	if err != nil || cancelResult == nil || cancelResult.Outcome != a2aapp.CancelApplied || cancelResult.Event == nil {
		t.Fatalf("cancel = %+v err=%v, want applied with a durable event", cancelResult, err)
	}
	if err := broker.PublishRemote(ctx, *cancelResult.Event); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not observe the remote cancellation")
	}
	terminal := false
	for _, ev := range c.snapshot() {
		if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok && s.Status.State == a2a.TaskStateCanceled {
			terminal = true
		}
	}
	if !terminal {
		t.Fatalf("cancel stream never emitted the canceled status: %#v", c.snapshot())
	}
}

// TestProjector_TerminalVisibilityRequiresResolution guards the projection
// invariant that a completed Task is never visible without its terminal
// Resolution: projecting a completed record with a nil Resolution must not
// fabricate fallback artifacts and claim success.
func TestProjector_TerminalVisibilityRequiresResolution(t *testing.T) {
	_, db, repo, _ := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusResolved, succeeded())
	// Deliberately no seedResolution: the terminal result row is missing.

	record, err := repo.GetTaskRecord(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	task := a2aapp.NewTaskProjector(redact.New("sk-secret")).Project(record, 1, true)
	if task.Status.State == a2a.TaskStateCompleted && record.Resolution == nil {
		if len(task.Artifacts) != 0 {
			t.Fatal("completed task fabricated artifacts before terminal result was committed")
		}
	}
}

func TestStreamProjector_ConsumerCancellationLeavesJobRunning(t *testing.T) {
	stream, db, repo, _ := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	c := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(c.snapshot()) >= 1 })
	cancel()
	<-c.done

	var job magi.DecisionJobModel
	if err := db.Where("case_id = ?", "case-1").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != string(entity.DecisionJobRunning) {
		t.Fatalf("job status after stream cancel = %s, want running", job.Status)
	}
}

func TestStreamProjector_FailsFastOnStreamLimit(t *testing.T) {
	stream, db, repo, _ := newStreamHarness(t, 1)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := startCollect(ctx, stream.Events(ctx, 7, "case-1"))
	waitFor(t, time.Second, func() bool { return len(first.snapshot()) >= 1 })

	var gotErr error
	for _, err := range stream.Events(ctx, 7, "case-2") {
		gotErr = err
		break
	}
	if !errors.Is(gotErr, a2aapp.ErrStreamLimitExceeded) {
		t.Fatalf("second stream error = %v, want ErrStreamLimitExceeded", gotErr)
	}
	cancel()
	<-first.done
}

func collectAll(t *testing.T, ctx context.Context, seq iter.Seq2[a2a.Event, error]) []a2a.Event {
	t.Helper()
	var out []a2a.Event
	for ev, err := range seq {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

type erringTaskRepo struct {
	a2aapp.SubmissionRepository
	err error
}

func (r *erringTaskRepo) GetTaskRecord(context.Context, int64, string) (*a2aapp.TaskRecord, error) {
	return nil, r.err
}

// TestStreamProjector_InternalErrorsAreHidden guards error disclosure: a
// repository failure (SQL text, paths, tool responses) must never reach the
// client over the protocol; the stream yields only a stable ErrInternalError.
func TestStreamProjector_InternalErrorsAreHidden(t *testing.T) {
	_, db, repo, broker := newStreamHarness(t, 8)
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())
	rawErr := errors.New("sql: no such table /var/lib/mysql/magi_event for case-1")
	erring := &erringTaskRepo{SubmissionRepository: repo, err: rawErr}
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	stream := a2aapp.NewDurableStreamProjector(erring, broker, broker, proj, 8, time.Millisecond)

	var gotErr error
	for _, err := range stream.Events(context.Background(), 7, "case-1") {
		gotErr = err
		break
	}
	if gotErr == nil {
		t.Fatal("expected an internal stream error")
	}
	if !errors.Is(gotErr, a2a.ErrInternalError) {
		t.Fatalf("error = %v, want ErrInternalError", gotErr)
	}
	if strings.Contains(gotErr.Error(), rawErr.Error()) || strings.Contains(gotErr.Error(), "magi_event") {
		t.Fatalf("internal error leaked to the protocol: %v", gotErr)
	}
}
