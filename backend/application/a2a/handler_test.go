package a2aapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/redact"
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
