package a2aapp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
	"time"
)

type captureLivePublisher struct {
	mu     sync.Mutex
	events []entity.MagiEvent
}

func (p *captureLivePublisher) PublishLive(_ context.Context, e entity.MagiEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
	return nil
}

func (p *captureLivePublisher) snapshot() []entity.MagiEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]entity.MagiEvent(nil), p.events...)
}

type memAuditRepo struct {
	mu     sync.Mutex
	events []*entity.AuditEvent
}

func (m *memAuditRepo) Record(_ context.Context, e *entity.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *memAuditRepo) List(_ context.Context, _, _ int) ([]*entity.AuditEvent, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]*entity.AuditEvent(nil), m.events...)
	return out, int64(len(out)), nil
}

func (m *memAuditRepo) snapshot() []*entity.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*entity.AuditEvent(nil), m.events...)
}

func hashMessageID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func newAuditHarness(t *testing.T) (*testHarness, *memAuditRepo, context.Context) {
	t.Helper()
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	repo := magi.NewA2ASubmissionRepository(db)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs})
	// Join the fire-and-forget worker before the test's temp dir is cleaned:
	// RunManager.Start returns immediately, so an unjoined worker can still be
	// settling (writing the job row) after the test body finished.
	t.Cleanup(rm.Shutdown)
	parser := a2aapp.NewInputParser(65536, 16)
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	svc := a2aapp.NewSubmissionService(parser, repo, rm, proj, 3)
	auditRepo := &memAuditRepo{}
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, nil,
		a2aapp.WithHandlerAudit(audit.NewService(auditRepo)))
	_ = tracing.NewProvider(tracing.Config{Enabled: true}, nil)
	ctx, span := tracing.Start(context.Background(), "audit-parent")
	_ = span
	return &testHarness{handler: handler, repo: repo, rm: rm, orch: orch, db: db}, auditRepo, ctx
}

// TestHandler_AuditRecordsOutcomesWithExternalMessageID guards the durable
// audit trail: Send success and failure plus Cancel failure are all recorded,
// the external A2A message ID is hashed (never a derived internal ID), and the
// trace field carries the real OTel trace ID rather than a fabricated UUID.
func TestHandler_AuditRecordsOutcomesWithExternalMessageID(t *testing.T) {
	h, auditRepo, ctx := newAuditHarness(t)
	result, err := h.handler.SendMessage(ctx, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	task, ok := result.(*a2a.Task)
	if !ok || task == nil {
		t.Fatalf("send result = %#v, want a Task", result)
	}
	if _, err := h.handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: &a2a.Message{
		ID: "msg-2", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("  ")},
	}}); err == nil {
		t.Fatal("expected invalid send to fail")
	}
	if _, err := h.handler.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID("case-missing")}); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("cancel error = %v, want ErrTaskNotFound", err)
	}

	events := auditRepo.snapshot()
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want 3", len(events))
	}
	success, failSend, cancel := events[0], events[1], events[2]
	if success.Action != "a2a.send" || success.Status != 200 || success.Resource != string(task.ID) {
		t.Fatalf("success audit = %+v", success)
	}
	if !strings.Contains(success.Detail, "message="+hashMessageID("msg-1")) {
		t.Fatalf("success audit detail missing external message hash: %q", success.Detail)
	}
	if tracePart := strings.TrimPrefix(success.Detail, "trace="); strings.Contains(success.Detail, "trace=") {
		tracePart = strings.Split(tracePart, " ")[0]
		if strings.Contains(tracePart, "-") || len(tracePart) != 32 {
			t.Fatalf("audit used a fabricated/non-trace id: %q", success.Detail)
		}
	}
	if failSend.Action != "a2a.send" || failSend.Status != 400 {
		t.Fatalf("failed send audit = %+v", failSend)
	}
	if !strings.Contains(failSend.Detail, "message="+hashMessageID("msg-2")) {
		t.Fatalf("failed send audit detail missing message hash: %q", failSend.Detail)
	}
	if cancel.Action != "a2a.cancel" || cancel.Status != 404 || cancel.Resource != "case-missing" {
		t.Fatalf("cancel audit = %+v", cancel)
	}
}

var _ a2asrv.RequestHandler = (*a2aapp.Handler)(nil)

type testHarness struct {
	handler *a2aapp.Handler
	repo    a2aapp.SubmissionRepository
	rm      *decision.RunManager
	orch    *blockingOrch
	db      *gorm.DB
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	repo := magi.NewA2ASubmissionRepository(db)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs})
	t.Cleanup(rm.Shutdown)
	parser := a2aapp.NewInputParser(65536, 16)
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	svc := a2aapp.NewSubmissionService(parser, repo, rm, proj, 3)
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, nil)
	return &testHarness{handler: handler, repo: repo, rm: rm, orch: orch, db: db}
}

func principalCtx(userID int64) context.Context {
	return auth.WithPrincipal(context.Background(), &auth.Principal{UserID: userID, Name: "user"})
}

func TestHandler_SendMessageReturnsTask(t *testing.T) {
	h := newTestHarness(t)
	result, err := h.handler.SendMessage(principalCtx(7), submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	task, ok := result.(*a2a.Task)
	if !ok || task == nil || task.ID == "" {
		t.Fatalf("send result = %#v", result)
	}
	if task.ContextID == "" {
		t.Fatalf("task context id is empty: %+v", task)
	}
	h.rm.Cancel(string(task.ID))
}

func TestHandler_GetTaskIsOwnerScopedAndMasksForeign(t *testing.T) {
	h := newTestHarness(t)
	result, err := h.handler.SendMessage(principalCtx(7), submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	taskID := string(result.(*a2a.Task).ID)
	got, err := h.handler.GetTask(principalCtx(7), &a2a.GetTaskRequest{ID: a2a.TaskID(taskID), HistoryLength: intPtr(1)})
	if err != nil || got == nil || got.ID != a2a.TaskID(taskID) {
		t.Fatalf("get task = %+v err=%v", got, err)
	}
	if _, err := h.handler.GetTask(principalCtx(8), &a2a.GetTaskRequest{ID: a2a.TaskID(taskID)}); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("foreign get error = %v, want ErrTaskNotFound", err)
	}
	if _, err := h.handler.GetTask(principalCtx(7), &a2a.GetTaskRequest{ID: "case-missing"}); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("missing get error = %v, want ErrTaskNotFound", err)
	}
	h.rm.Cancel(taskID)
}

func TestHandler_ListTasksPaginatesWithToken(t *testing.T) {
	h := newTestHarness(t)
	for i := 0; i < 3; i++ {
		if _, err := h.handler.SendMessage(principalCtx(7), submissionReq(msgID(i))); err != nil {
			t.Fatal(err)
		}
	}
	first, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{PageSize: 2, IncludeArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Tasks) != 2 || first.TotalSize != 3 || first.PageSize != 2 || first.NextPageToken == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{PageSize: 2, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Tasks) != 1 || second.NextPageToken != "" {
		t.Fatalf("second page = %+v", second)
	}
	seen := make(map[string]bool)
	for _, task := range append(first.Tasks, second.Tasks...) {
		if seen[string(task.ID)] {
			t.Fatalf("duplicate task %s across pages", task.ID)
		}
		seen[string(task.ID)] = true
	}
	for _, task := range append(first.Tasks, second.Tasks...) {
		h.rm.Cancel(string(task.ID))
	}
}

func TestHandler_ListTasksRejectsBadTokenAndBadPageSize(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{PageToken: "!!not-a-cursor"}); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("bad token error = %v, want ErrInvalidParams", err)
	}
	if _, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{PageSize: -1}); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("bad page size error = %v, want ErrInvalidParams", err)
	}
}

func TestHandler_ListTasksRejectsUnsupportedStatus(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{Status: a2a.TaskState("TASK_STATE_UNKNOWN")}); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("unsupported status error = %v, want ErrInvalidParams", err)
	}
	// Supported states and an empty status still reach the repository.
	for _, state := range []a2a.TaskState{
		"", a2a.TaskStateSubmitted, a2a.TaskStateWorking, a2a.TaskStateCompleted,
		a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected,
	} {
		if _, err := h.handler.ListTasks(principalCtx(7), &a2a.ListTasksRequest{Status: state}); err != nil {
			t.Fatalf("supported status %q rejected: %v", state, err)
		}
	}
}

func TestHandler_CancelTaskIsIdempotent(t *testing.T) {
	h := newTestHarness(t)
	result, err := h.handler.SendMessage(principalCtx(7), submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	taskID := string(result.(*a2a.Task).ID)
	<-h.orch.started

	canceled, err := h.handler.CancelTask(principalCtx(7), &a2a.CancelTaskRequest{ID: a2a.TaskID(taskID)})
	if err != nil {
		t.Fatal(err)
	}
	if canceled == nil || canceled.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("canceled task = %+v", canceled)
	}
	again, err := h.handler.CancelTask(principalCtx(7), &a2a.CancelTaskRequest{ID: a2a.TaskID(taskID)})
	if err != nil {
		t.Fatalf("repeated cancel: %v", err)
	}
	if again == nil || again.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("repeated cancel task = %+v", again)
	}
}

func TestHandler_CancelTaskTerminalIsNotCancelable(t *testing.T) {
	h := newTestHarness(t)
	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := h.repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.handler.CancelTask(principalCtx(7), &a2a.CancelTaskRequest{ID: "case-1"}); !errors.Is(err, a2a.ErrTaskNotCancelable) {
		t.Fatalf("terminal cancel error = %v, want ErrTaskNotCancelable", err)
	}
}

// TestHandler_CancelTaskFansOutDurableEvent guards the cross-replica cancel
// path: after the repository transaction commits a durable CANCELLED event,
// the handler fans it out to the live publisher for in-process streams.
func TestHandler_CancelTaskFansOutDurableEvent(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	repo := magi.NewA2ASubmissionRepository(db)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs})
	t.Cleanup(rm.Shutdown)
	parser := a2aapp.NewInputParser(65536, 16)
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	svc := a2aapp.NewSubmissionService(parser, repo, rm, proj, 3)
	live := &captureLivePublisher{}
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, nil,
		a2aapp.WithHandlerLivePublisher(live))
	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusInvestigating, running())

	task, err := handler.CancelTask(principalCtx(7), &a2a.CancelTaskRequest{ID: a2a.TaskID("case-1")})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if task == nil || task.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("cancel task = %+v, want canceled", task)
	}
	events := live.snapshot()
	if len(events) != 1 {
		t.Fatalf("live fanout events = %d, want 1", len(events))
	}
	if events[0].Type != entity.EventCaseStatusChanged || events[0].Seq == 0 || events[0].CaseID != "case-1" {
		t.Fatalf("live fanout event = %+v, want durable CANCELLED status event", events[0])
	}
	var payload map[string]any
	if len(events[0].Payload) > 0 {
		if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
	}
	if payload["status"] != string(entity.CaseStatusCancelled) {
		t.Fatalf("fanout status = %v, want CANCELLED", payload["status"])
	}
}

func TestHandler_SendMessageMapsInputErrors(t *testing.T) {
	h := newTestHarness(t)
	req := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "msg-1", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("  ")}}}
	if _, err := h.handler.SendMessage(principalCtx(7), req); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("empty text error = %v, want ErrInvalidParams", err)
	}
	raw := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "msg-2", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewRawPart([]byte("x"))}}}
	if _, err := h.handler.SendMessage(principalCtx(7), raw); !errors.Is(err, a2a.ErrUnsupportedContentType) {
		t.Fatalf("raw part error = %v, want ErrUnsupportedContentType", err)
	}
	taskMsg := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "msg-3", TaskID: "task-x", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}}
	if _, err := h.handler.SendMessage(principalCtx(7), taskMsg); !errors.Is(err, a2a.ErrUnsupportedOperation) {
		t.Fatalf("task message error = %v, want ErrUnsupportedOperation", err)
	}
}

func TestHandler_SendMessageRejectsIdempotencyConflict(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.handler.SendMessage(principalCtx(7), submissionReq("msg-1")); err != nil {
		t.Fatal(err)
	}
	conflict := submissionReq("msg-1")
	conflict.Message.Parts = a2a.ContentParts{a2a.NewTextPart("different question")}
	if _, err := h.handler.SendMessage(principalCtx(7), conflict); !errors.Is(err, a2a.ErrInvalidRequest) {
		t.Fatalf("idempotency conflict error = %v, want ErrInvalidRequest", err)
	}
}

func TestHandler_UnsupportedMethods(t *testing.T) {
	h := newTestHarness(t)
	ctx := principalCtx(7)
	if _, err := h.handler.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigRequest{}); !errors.Is(err, a2a.ErrPushNotificationNotSupported) {
		t.Fatalf("get push config = %v", err)
	}
	if _, err := h.handler.ListTaskPushConfigs(ctx, &a2a.ListTaskPushConfigRequest{}); !errors.Is(err, a2a.ErrPushNotificationNotSupported) {
		t.Fatalf("list push configs = %v", err)
	}
	if _, err := h.handler.CreateTaskPushConfig(ctx, &a2a.PushConfig{}); !errors.Is(err, a2a.ErrPushNotificationNotSupported) {
		t.Fatalf("create push config = %v", err)
	}
	if err := h.handler.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigRequest{}); !errors.Is(err, a2a.ErrPushNotificationNotSupported) {
		t.Fatalf("delete push config = %v", err)
	}
	if _, err := h.handler.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{}); !errors.Is(err, a2a.ErrExtendedCardNotConfigured) {
		t.Fatalf("extended card = %v", err)
	}
}

func TestHandler_SubscribeToTaskDelegatesToStream(t *testing.T) {
	db := openSubmissionDB(t)
	repo := magi.NewA2ASubmissionRepository(db)
	broker := newTestEventBroker()
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	stream := a2aapp.NewDurableStreamProjector(repo, broker, broker, proj, 8, 10*time.Millisecond)
	rm := decision.NewRunManager(newBlockingOrch())
	t.Cleanup(rm.Shutdown)
	svc := a2aapp.NewSubmissionService(a2aapp.NewInputParser(65536, 16), repo, rm, proj, 3)
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, stream)

	seedStreamTask(t, db, repo, "case-1", "conv-1", entity.CaseStatusResolved, succeeded())
	seedResolution(t, db, "case-1")

	var events []a2a.Event
	for ev, err := range handler.SubscribeToTask(principalCtx(7), &a2a.SubscribeToTaskRequest{ID: "case-1"}) {
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("subscribe events = %d, want 1", len(events))
	}
	task, ok := events[0].(*a2a.Task)
	if !ok || task.Status.State != a2a.TaskStateCompleted || len(task.Artifacts) != 2 {
		t.Fatalf("subscribed snapshot = %#v", events[0])
	}

	for _, err := range handler.SubscribeToTask(principalCtx(8), &a2a.SubscribeToTaskRequest{ID: "case-1"}) {
		if !errors.Is(err, a2a.ErrTaskNotFound) {
			t.Fatalf("foreign subscribe error = %v, want ErrTaskNotFound", err)
		}
		break
	}
}

func msgID(i int) string {
	return "msg-" + string(rune('a'+i))
}

func intPtr(v int) *int { return &v }

// TestHandler_SendStreamingMessageUnsupportedStreamingAudits400 guards the
// streaming audit mapping: an unconfigured stream must be recorded as a
// client-side 400 (unsupported operation), not a 500.
func TestHandler_SendStreamingMessageUnsupportedStreamingAudits400(t *testing.T) {
	h, auditRepo, ctx := newAuditHarness(t)

	var gotErr error
	for _, err := range h.handler.SendStreamingMessage(ctx, submissionReq("msg-unsupported")) {
		gotErr = err
		break
	}
	var ae *a2a.Error
	if !errors.As(gotErr, &ae) || ae.Err != a2a.ErrUnsupportedOperation {
		t.Fatalf("streaming error = %v, want ErrUnsupportedOperation", gotErr)
	}
	events := auditRepo.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Action != "a2a.send" || events[0].Status != 400 {
		t.Fatalf("unsupported streaming audit = %+v", events[0])
	}
}

// TestHandler_SendStreamingMessageInvalidInputAudits400 guards the streaming
// audit mapping: submit-side invalid input must be recorded as a client-side
// 400 (invalid params), not the raw application error that would map to 500.
func TestHandler_SendStreamingMessageInvalidInputAudits400(t *testing.T) {
	db := openSubmissionDB(t)
	repo := magi.NewA2ASubmissionRepository(db)
	broker := newTestEventBroker()
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	stream := a2aapp.NewDurableStreamProjector(repo, broker, broker, proj, 8, time.Hour)
	jobs := newFakeJobRepo()
	rm := decision.NewRunManager(newBlockingOrch(), decision.RunManagerDeps{JobRepo: jobs})
	t.Cleanup(rm.Shutdown)
	svc := a2aapp.NewSubmissionService(a2aapp.NewInputParser(65536, 16), repo, rm, proj, 3)
	auditRepo := &memAuditRepo{}
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, stream,
		a2aapp.WithHandlerAudit(audit.NewService(auditRepo)))

	req := &a2a.SendMessageRequest{Message: &a2a.Message{
		ID: "msg-invalid", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("  ")},
	}}
	var gotErr error
	for _, err := range handler.SendStreamingMessage(principalCtx(7), req) {
		gotErr = err
		break
	}
	var ae *a2a.Error
	if !errors.As(gotErr, &ae) || ae.Err != a2a.ErrInvalidParams {
		t.Fatalf("streaming error = %v, want ErrInvalidParams", gotErr)
	}
	events := auditRepo.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Action != "a2a.send" || events[0].Status != 400 {
		t.Fatalf("invalid input streaming audit = %+v", events[0])
	}
}
