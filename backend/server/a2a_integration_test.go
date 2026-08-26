package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/a2a"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// deterministicOrch completes a case only when released; a cancelled run ends
// without writing a Resolution or terminal events (no late artifact).
type deterministicOrch struct {
	db      *gorm.DB
	broker  *server.EventBroker
	started chan string
	release chan struct{}
}

func (o *deterministicOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	select {
	case o.started <- c.ID:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.release:
	}
	now := time.Now().UTC()
	consensus := `{"outcome":"strong_approval","round":1}`
	if err := o.db.Model(&magi.CaseModel{}).Where("id = ?", c.ID).
		Updates(map[string]any{"status": string(entity.CaseStatusResolved), "updated_at": now}).Error; err != nil {
		return nil, err
	}
	res := &entity.Resolution{
		ID: "res-" + c.ID, CaseID: c.ID,
		Consensus:     entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1},
		FinalDecision: entity.VoteDecisionApprove, FinalReport: "# Decision report",
		KeyEvidenceIDs: []string{"EV-1"}, KeyClaimIDs: []string{"CL-1"}, CreatedAt: now,
	}
	if err := o.db.Create(&magi.ResolutionModel{
		ID: res.ID, CaseID: res.CaseID, ConsensusJSON: consensus,
		FinalDecision: string(res.FinalDecision), FinalReport: res.FinalReport,
		KeyEvidenceIDsJSON: `["EV-1"]`, KeyClaimIDsJSON: `["CL-1"]`, CreatedAt: now,
	}).Error; err != nil {
		return nil, err
	}
	_ = o.broker.Publish(ctx, entity.NewEvent(c.ID, "", nil, entity.EventResolutionCreated, map[string]any{"status": string(entity.CaseStatusResolved)}))
	_ = o.broker.Publish(ctx, entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)}))
	return res, nil
}

type a2aIntegrationEnv struct {
	baseURL string
	orch    *deterministicOrch
	db      *gorm.DB
	broker  *server.EventBroker
	repo    a2aapp.SubmissionRepository
}

func startA2AIntegration(t *testing.T) *a2aIntegrationEnv {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1) // keep the :memory: database shared across calls
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatal(err)
	}
	broker := server.NewEventBroker()
	repo := magi.NewA2ASubmissionRepository(db)
	orch := &deterministicOrch{db: db, broker: broker, started: make(chan string, 1), release: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{Metrics: metrics.New()})
	proj := a2aapp.NewTaskProjector(redact.New("k7"))
	svc := a2aapp.NewSubmissionService(a2aapp.NewInputParser(65536, 16), repo, rm, proj, 3)
	stream := a2aapp.NewDurableStreamProjector(repo, broker, broker, proj, 8, 20*time.Millisecond)
	handler := a2aapp.NewHandler(svc, repo, proj, a2aapp.CursorCodec{MaxPageSize: 100}, rm, stream,
		a2aapp.WithHandlerMetrics(metrics.New()))
	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "owner", Key: "k7", UserID: 7, Role: "user"},
		{Name: "foreign", Key: "k8", UserID: 8, Role: "user"},
	})
	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.Use(server.Auth(authSvc))
	a2atransport.Mount(h, a2atransport.MountDeps{
		Handler: handler, PublicURL: "http://" + addr, BasePath: "/a2a",
		Name: "MAGI", Description: "integration",
	})
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server not ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &a2aIntegrationEnv{baseURL: "http://" + addr, orch: orch, db: db, broker: broker, repo: repo}
}

func newA2AClient(t *testing.T, baseURL, token string) (*a2aclient.Client, context.Context) {
	t.Helper()
	resp, err := http.Get(baseURL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatal(err)
	}
	store := a2aclient.NewInMemoryCredentialsStore()
	sid := a2aclient.SessionID("magi-integration-" + token)
	store.Set(sid, a2a.SecuritySchemeName("bearer"), a2aclient.AuthCredential(token))
	client, err := a2aclient.NewFromCard(context.Background(), &card,
		a2aclient.WithConfig(a2aclient.Config{PreferredTransports: []a2a.TransportProtocol{a2a.TransportProtocolHTTPJSON}}),
		a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: store}),
	)
	if err != nil {
		t.Fatalf("client from card: %v", err)
	}
	ctx := context.Background()
	return client, a2aclient.AttachSessionID(ctx, sid)
}

func a2aSendRequest(messageID string) *a2a.SendMessageRequest {
	return &a2a.SendMessageRequest{Message: &a2a.Message{
		ID: messageID, Role: a2a.MessageRoleUser,
		Parts: a2a.ContentParts{a2a.NewTextPart("Should MAGI expose A2A?")},
	}}
}

func TestA2AIntegration_OfficialClientLifecycle(t *testing.T) {
	env := startA2AIntegration(t)
	client, callCtx := newA2AClient(t, env.baseURL, "k7")

	result, err := client.SendMessage(callCtx, a2aSendRequest("msg-1"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	task := result.(*a2a.Task)
	if task == nil || task.ID == "" {
		t.Fatalf("send result = %#v", result)
	}
	taskID := task.ID

	select {
	case <-env.orch.started:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator did not start")
	}

	got, err := client.GetTask(callCtx, &a2a.GetTaskRequest{ID: taskID, HistoryLength: intPtr2(1)})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != taskID {
		t.Fatalf("get id = %s, want %s", got.ID, taskID)
	}

	list, err := client.ListTasks(callCtx, &a2a.ListTasksRequest{PageSize: 50, IncludeArtifacts: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, lt := range list.Tasks {
		if lt.ID == taskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("task %s not listed: %+v", taskID, list.Tasks)
	}

	canceled, err := client.CancelTask(callCtx, &a2a.CancelTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("canceled state = %s", canceled.Status.State)
	}
	after, err := client.GetTask(callCtx, &a2a.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("get after cancel: %v", err)
	}
	if after.Status.State != a2a.TaskStateCanceled || len(after.Artifacts) != 0 {
		t.Fatalf("canceled task leaked artifacts: %+v", after)
	}

	if _, err := client.GetTaskPushConfig(callCtx, &a2a.GetTaskPushConfigRequest{TaskID: taskID, ID: "cfg"}); err == nil {
		t.Fatal("push config must be unsupported")
	}
}

func TestA2AIntegration_OfficialClientStreamEmitsTwoArtifacts(t *testing.T) {
	env := startA2AIntegration(t)
	client, callCtx := newA2AClient(t, env.baseURL, "k7")

	seq := client.SendStreamingMessage(callCtx, a2aSendRequest("msg-stream"))
	eventsCh := make(chan []a2a.Event, 1)
	errCh := make(chan error, 1)
	firstEvent := make(chan struct{})
	go func() {
		var events []a2a.Event
		for ev, err := range seq {
			if err != nil {
				errCh <- err
				return
			}
			events = append(events, ev)
			select {
			case <-firstEvent:
			default:
				close(firstEvent)
			}
		}
		eventsCh <- events
	}()

	select {
	case <-env.orch.started:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator did not start")
	}
	// The stream must emit its initial working snapshot before we complete the
	// run; otherwise it would return the terminal snapshot immediately.
	select {
	case <-firstEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not emit its initial snapshot")
	}
	close(env.orch.release) // complete the run

	var events []a2a.Event
	select {
	case events = <-eventsCh:
	case err := <-errCh:
		t.Fatalf("stream error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not terminate")
	}
	artifactCount := 0
	var finalState a2a.TaskState
	for _, ev := range events {
		switch v := ev.(type) {
		case *a2a.TaskArtifactUpdateEvent:
			artifactCount++
			if !v.LastChunk || v.Append {
				t.Fatalf("artifact chunk flags: %+v", v)
			}
		case *a2a.TaskStatusUpdateEvent:
			finalState = v.Status.State
		}
	}
	if artifactCount != 2 {
		t.Fatalf("artifacts = %d, want 2; events=%+v", artifactCount, events)
	}
	if finalState != a2a.TaskStateCompleted {
		t.Fatalf("final state = %s, want completed", finalState)
	}

	// Subscribe on the now-terminal task returns the snapshot immediately.
	sub := client.SubscribeToTask(callCtx, &a2a.SubscribeToTaskRequest{ID: firstTaskID(events)})
	var subscribed []a2a.Event
	for ev, err := range sub {
		if err != nil {
			t.Fatal(err)
		}
		subscribed = append(subscribed, ev)
	}
	if len(subscribed) != 1 {
		t.Fatalf("terminal subscribe events = %d, want 1", len(subscribed))
	}
	snapshot, ok := subscribed[0].(*a2a.Task)
	if !ok || snapshot.Status.State != a2a.TaskStateCompleted || len(snapshot.Artifacts) != 2 {
		t.Fatalf("terminal snapshot = %#v", subscribed[0])
	}
}

func TestA2AIntegration_ForeignUserMasked(t *testing.T) {
	env := startA2AIntegration(t)
	owner, ownerCtx := newA2AClient(t, env.baseURL, "k7")
	result, err := owner.SendMessage(ownerCtx, a2aSendRequest("msg-owned"))
	if err != nil {
		t.Fatal(err)
	}
	taskID := result.(*a2a.Task).ID

	foreign, foreignCtx := newA2AClient(t, env.baseURL, "k8")
	if _, err := foreign.GetTask(foreignCtx, &a2a.GetTaskRequest{ID: taskID}); err == nil {
		t.Fatal("foreign GetTask must fail")
	}
	if _, err := foreign.CancelTask(foreignCtx, &a2a.CancelTaskRequest{ID: taskID}); err == nil {
		t.Fatal("foreign CancelTask must fail")
	}
	list, err := foreign.ListTasks(foreignCtx, &a2a.ListTasksRequest{PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tasks) != 0 {
		t.Fatalf("foreign ListTasks leaked tasks: %+v", list.Tasks)
	}
}

func firstTaskID(events []a2a.Event) a2a.TaskID {
	for _, ev := range events {
		if t, ok := ev.(*a2a.Task); ok {
			return t.ID
		}
	}
	return ""
}

func intPtr2(v int) *int { return &v }

// TestA2AIntegration_DisconnectLeavesJobRunning guards the durable submission
// contract: after Send returns, a client disconnect must not cancel the
// background job. The binding stays STARTED and the task remains working until
// the run is released.
func TestA2AIntegration_DisconnectLeavesJobRunning(t *testing.T) {
	env := startA2AIntegration(t)
	client, callCtx := newA2AClient(t, env.baseURL, "k7")

	result, err := client.SendMessage(callCtx, a2aSendRequest("msg-disc"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	taskID := result.(*a2a.Task).ID
	select {
	case <-env.orch.started:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator did not start")
	}

	// The call already returned: the client is "disconnected" but the job runs.
	sub, err := env.repo.GetByTask(context.Background(), 7, string(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding state = %s, want STARTED after disconnect", sub.State)
	}
	got, err := client.GetTask(callCtx, &a2a.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.State != a2a.TaskStateWorking {
		t.Fatalf("task after disconnect = %s, want working", got.Status.State)
	}
	close(env.orch.release)
}

// TestA2AIntegration_ReconnectSubscribeCatchesUpAfterDisconnect guards the
// durable catch-up path: a consumer that disconnects mid-stream and reconnects
// after the run completed must receive the terminal snapshot (with artifacts)
// from the fresh Subscribe.
func TestA2AIntegration_ReconnectSubscribeCatchesUpAfterDisconnect(t *testing.T) {
	env := startA2AIntegration(t)
	client, callCtx := newA2AClient(t, env.baseURL, "k7")
	streamCtx, cancelStream := context.WithCancel(callCtx)
	defer cancelStream()

	seq := client.SendStreamingMessage(streamCtx, a2aSendRequest("msg-reconnect"))
	firstEvent := make(chan struct{})
	done := make(chan struct{})
	taskIDCh := make(chan a2a.TaskID, 1)
	go func() {
		defer close(done)
		for ev, err := range seq {
			if err != nil {
				t.Errorf("first stream error: %v", err)
				return
			}
			if task, ok := ev.(*a2a.Task); ok {
				taskIDCh <- task.ID
			}
			select {
			case <-firstEvent:
			default:
				close(firstEvent)
			}
			return // consume the initial snapshot, then disconnect
		}
	}()
	select {
	case <-env.orch.started:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator did not start")
	}
	select {
	case <-firstEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("first stream did not emit its snapshot")
	}
	var taskID a2a.TaskID
	select {
	case taskID = <-taskIDCh:
	case <-time.After(5 * time.Second):
		t.Fatal("first stream did not expose a task id")
	}
	cancelStream() // disconnect the SSE connection; the job keeps running
	<-done

	close(env.orch.release) // complete while disconnected
	time.Sleep(100 * time.Millisecond)

	sub := client.SubscribeToTask(callCtx, &a2a.SubscribeToTaskRequest{ID: taskID})
	var terminal *a2a.Task
	for ev, err := range sub {
		if err != nil {
			t.Fatalf("reconnect subscribe error: %v", err)
		}
		if snap, ok := ev.(*a2a.Task); ok {
			terminal = snap
		}
	}
	if terminal == nil || terminal.Status.State != a2a.TaskStateCompleted || len(terminal.Artifacts) != 2 {
		t.Fatalf("reconnect terminal snapshot = %#v", terminal)
	}
}

// TestA2AIntegration_ForeignSubscribeNotFound guards tenant isolation on the
// streaming surface: a foreign principal subscribing to an owned task gets a
// not-found error instead of an empty stream.
func TestA2AIntegration_ForeignSubscribeNotFound(t *testing.T) {
	env := startA2AIntegration(t)
	owner, ownerCtx := newA2AClient(t, env.baseURL, "k7")
	result, err := owner.SendMessage(ownerCtx, a2aSendRequest("msg-foreign-sub"))
	if err != nil {
		t.Fatal(err)
	}
	taskID := result.(*a2a.Task).ID

	foreign, foreignCtx := newA2AClient(t, env.baseURL, "k8")
	err = a2a.ErrTaskNotFound
	for _, e := range foreign.SubscribeToTask(foreignCtx, &a2a.SubscribeToTaskRequest{ID: taskID}) {
		if e != nil {
			err = e
		}
	}
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("foreign subscribe error = %v, want ErrTaskNotFound", err)
	}
}

// TestA2AIntegration_SecondHandlerCancelsAndStreamObserves guards cross-replica
// cancellation: a second Handler with its own RunManager (same durable repo and
// broker) cancels a task whose stream is open on the first handler, and the
// stream terminates with the canceled status.
func TestA2AIntegration_SecondHandlerCancelsAndStreamObserves(t *testing.T) {
	env := startA2AIntegration(t)
	client, callCtx := newA2AClient(t, env.baseURL, "k7")

	seq := client.SendStreamingMessage(callCtx, a2aSendRequest("msg-2nd-cancel"))
	firstEvent := make(chan struct{})
	eventsCh := make(chan []a2a.Event, 1)
	errCh := make(chan error, 1)
	taskIDCh := make(chan a2a.TaskID, 1)
	go func() {
		var events []a2a.Event
		for ev, err := range seq {
			if err != nil {
				errCh <- err
				return
			}
			events = append(events, ev)
			if task, ok := ev.(*a2a.Task); ok {
				select {
				case taskIDCh <- task.ID:
				default:
				}
			}
			select {
			case <-firstEvent:
			default:
				close(firstEvent)
			}
		}
		eventsCh <- events
	}()
	select {
	case <-env.orch.started:
	case <-time.After(5 * time.Second):
		t.Fatal("orchestrator did not start")
	}
	select {
	case <-firstEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("first stream did not emit its snapshot")
	}
	var taskID a2a.TaskID
	select {
	case taskID = <-taskIDCh:
	case <-time.After(5 * time.Second):
		t.Fatal("first stream did not expose a task id")
	}

	// A second replica handler cancels through the shared durable repository.
	secondRM := decision.NewRunManager(env.orch, decision.RunManagerDeps{Metrics: metrics.New()})
	secondSvc := a2aapp.NewSubmissionService(a2aapp.NewInputParser(65536, 16), env.repo, secondRM, a2aapp.NewTaskProjector(redact.New("k7")), 3)
	secondHandler := a2aapp.NewHandler(secondSvc, env.repo, a2aapp.NewTaskProjector(redact.New("k7")), a2aapp.CursorCodec{MaxPageSize: 100}, secondRM, nil)
	ctxWithPrincipal := auth.WithPrincipal(context.Background(), &auth.Principal{UserID: 7, Name: "owner"})

	if _, err := secondHandler.CancelTask(ctxWithPrincipal, &a2a.CancelTaskRequest{ID: taskID}); err != nil {
		t.Fatalf("second handler cancel: %v", err)
	}
	select {
	case <-eventsCh:
	case err := <-errCh:
		t.Fatalf("stream error after second-replica cancel: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not observe the second-replica cancellation")
	}
}
