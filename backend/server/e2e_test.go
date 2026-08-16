package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stubKnowledge struct{}

func (k *stubKnowledge) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{}, nil
}

func (k *stubKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	return port.StoreStats{}, nil
}

type e2eOrch struct{ repo port.Repository }

func (o *e2eOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	res := &entity.Resolution{
		ID: "res-" + c.ID, CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove,
		Consensus: entity.ConsensusResult{Round: 1, Outcome: entity.ConsensusStrongApproval, Votes: []entity.Vote{{Decision: entity.VoteDecisionApprove, Confidence: 90}}},
		CreatedAt: time.Now(),
	}
	_ = o.repo.ResolutionRepo().Create(ctx, res)
	_ = o.repo.CaseRepo().UpdateStatus(ctx, c.ID, entity.CaseStatusResolved)
	_ = o.repo.AgentRunRepo().Create(ctx, &entity.AgentRun{
		ID: "run-" + c.ID, CaseID: c.ID, MagiCode: "melchior",
		Status: entity.AgentRunStatusCompleted,
		Usage:  &entity.Usage{TotalTokens: 100, CostUSD: 0.5},
	})
	_ = o.repo.MemoryRepo().Save(ctx, &entity.CaseMemoryProjection{CaseID: c.ID, QuestionSummary: c.Question, Resolution: "approve"})
	return res, nil
}

func openE2EDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newE2EServer(t *testing.T) *hzserver.Hertz {
	t.Helper()
	db := openE2EDB(t)
	repo := magi.NewRepository(db)
	orch := &e2eOrch{repo: repo}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{})
	reg := metrics.New()

	decSvc := decision.NewService(orch, decision.ServiceConfig{MaxDebateRounds: 1},
		decision.WithCaseRepo(repo.CaseRepo()),
		decision.WithResolutionRepo(repo.ResolutionRepo()),
		decision.WithEvidenceRepo(repo.EvidenceRepo()),
		decision.WithClaimRepo(repo.ClaimRepo()),
		decision.WithVoteRepo(repo.VoteRepo()),
		decision.WithAgentRunRepo(repo.AgentRunRepo()),
		decision.WithToolCallRepo(repo.ToolCallRepo()),
		decision.WithRunManager(rm))

	dsSvc := dataset.NewService(magi.NewDatasetRepository(db), repo.CaseRepo(), orch, 1)
	plugsSvc := plugins.NewService(magi.NewPluginBindingRepository(db))
	admSvc := admin.NewService(repo.CaseRepo(), repo.AgentRunRepo())
	recSvc := recurring.NewService(magi.NewRecurringRepository(db), repo.CaseRepo(), rm, 1)
	askSvc := assistant.NewService(decSvc)
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "admin", Key: "k7", UserID: 7, Role: "admin"}, {Name: "u8", Key: "k8", UserID: 8, Role: "user"}})
	broker := server.NewEventBroker()
	knowledge := &stubKnowledge{}

	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	server.RegisterRoutesWithDeps(h, server.RouteDeps{
		Decision:   decSvc,
		Auth:       authSvc,
		Metrics:    reg,
		Dataset:    dsSvc,
		Plugins:    plugsSvc,
		Admin:      admSvc,
		Recurring:  recSvc,
		Assistant:  askSvc,
		Replay:     replay.NewService(repo.EventRepo()),
		Evaluation: evaluation.NewService(evaluation.WithRepository(repo)),
		Memory:     memory.NewService(knowledge, repo.MemoryRepo()),
		Tool:       tool.NewService(nil),
		Broker:     broker,
		EventRepo:  repo.EventRepo(),
	})
	return h
}

func get(t *testing.T, h *hzserver.Hertz, path, token string) (int, string) {
	t.Helper()
	var w *ut.ResponseRecorder
	if token != "" {
		w = ut.PerformRequest(h.Engine, "GET", path, nil, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	} else {
		w = ut.PerformRequest(h.Engine, "GET", path, nil)
	}
	return w.Code, w.Body.String()
}

func post(t *testing.T, h *hzserver.Hertz, path, body, token string) (int, string) {
	t.Helper()
	b := []byte(body)
	var w *ut.ResponseRecorder
	if token != "" {
		w = ut.PerformRequest(h.Engine, "POST", path, &ut.Body{Body: bytes.NewBuffer(b), Len: len(b)}, ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Content-Type", Value: "application/json"})
	} else {
		w = ut.PerformRequest(h.Engine, "POST", path, &ut.Body{Body: bytes.NewBuffer(b), Len: len(b)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return w.Code, w.Body.String()
}

func field(t *testing.T, body, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	v, _ := m[key].(string)
	return v
}

func waitFor(t *testing.T, h *hzserver.Hertz, path, token, key, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, body := get(t, h, path, token)
		if strings.Contains(body, `"`+key+`":"`+want+`"`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, body := get(t, h, path, token)
	t.Fatalf("%s never reached %s=%s: %s", path, key, want, body)
}

func TestE2E_HarnessFlow(t *testing.T) {
	h := newE2EServer(t)

	if code, _ := get(t, h, "/health", ""); code != 200 {
		t.Fatalf("health: %d", code)
	}

	// Auth gate
	if code, _ := post(t, h, "/api/v1/cases", `{"question":"ship?"}`, ""); code != 401 {
		t.Fatalf("create without token: %d", code)
	}

	// Case lifecycle
	code, body := post(t, h, "/api/v1/cases", `{"question":"Should we adopt Rust?","background":"team survey"}`, "k7")
	if code != 201 {
		t.Fatalf("create case: %d %s", code, body)
	}
	caseID := field(t, body, "id")
	code, _ = post(t, h, "/api/v1/cases/"+caseID+"/run", "", "k7")
	if code != 202 {
		t.Fatalf("run case: %d", code)
	}
	waitFor(t, h, "/api/v1/cases/"+caseID, "k7", "status", "RESOLVED")

	// Multi-tenant case-list isolation (P0: D2): user 8 must not see user 7's
	// case, user 7 sees their own, and limit/offset pagination is honored.
	if code, body = get(t, h, "/api/v1/cases", "k8"); code != 200 || strings.Contains(body, caseID) {
		t.Fatalf("user 8 must not see user 7's case: %d %s", code, body)
	}
	if code, body = get(t, h, "/api/v1/cases", "k7"); code != 200 || !strings.Contains(body, caseID) {
		t.Fatalf("user 7 should see own case: %d %s", code, body)
	}
	if code, body = get(t, h, "/api/v1/cases?limit=1&offset=0", "k7"); code != 200 || strings.Count(body, `"id":"case-`) != 1 {
		t.Fatalf("pagination limit=1 should return exactly 1 case: %d %s", code, body)
	}

	// Dataset benchmark
	code, body = post(t, h, "/api/v1/datasets", `{"name":"launch-eval","description":"launch decisions"}`, "k7")
	if code != 201 {
		t.Fatalf("create dataset: %d %s", code, body)
	}
	dsID := field(t, body, "id")
	code, body = post(t, h, "/api/v1/datasets/"+dsID+"/items", `{"items":[{"question":"ship A?","expected_decision":"approve"},{"question":"ship B?","expected_decision":"reject"}]}`, "k7")
	if code != 200 {
		t.Fatalf("add items: %d %s", code, body)
	}
	code, body = post(t, h, "/api/v1/datasets/"+dsID+"/runs", "", "k7")
	if code != 202 {
		t.Fatalf("start benchmark: %d %s", code, body)
	}
	runID := field(t, body, "id")
	waitFor(t, h, "/api/v1/benchmarks/"+runID, "k7", "status", "succeeded")
	_, detail := get(t, h, "/api/v1/benchmarks/"+runID, "k7")
	if !strings.Contains(detail, `"accuracy":0.5`) {
		t.Fatalf("benchmark accuracy: %s", detail)
	}

	// Plugin bindings
	code, body = post(t, h, "/api/v1/plugins", `{"plugin_id":1,"tool_id":2,"enabled":true}`, "k7")
	if code != 201 {
		t.Fatalf("create plugin binding: %d %s", code, body)
	}
	pbID := field(t, body, "id")
	code, body = get(t, h, "/api/v1/plugins", "k7")
	if code != 200 || !strings.Contains(body, `"plugin_id":1`) {
		t.Fatalf("list plugins: %d %s", code, body)
	}
	patchBody := fmt.Sprintf(`{"enabled":false}`)
	if w := ut.PerformRequest(h.Engine, "PATCH", "/api/v1/plugins/"+pbID, &ut.Body{Body: bytes.NewBuffer([]byte(patchBody)), Len: len(patchBody)}, ut.Header{Key: "Authorization", Value: "Bearer k7"}, ut.Header{Key: "Content-Type", Value: "application/json"}); w.Code != 200 {
		t.Fatalf("disable plugin: %d", w.Code)
	}

	// Metrics
	_, mbody := get(t, h, "/metrics", "")
	if !strings.Contains(mbody, "magi_cases_created_total 1") {
		t.Fatalf("metrics missing case counter:\n%s", mbody)
	}

	// Evaluation + feedback on benchmark results
	code, body = post(t, h, "/api/v1/evaluation/"+caseID, "", "k7")
	if code != 200 || !strings.Contains(body, `"first_round_consensus":true`) {
		t.Fatalf("evaluate: %d %s", code, body)
	}
	var detailResp struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	_, detailBody := get(t, h, "/api/v1/benchmarks/"+runID, "k7")
	if err := json.Unmarshal([]byte(detailBody), &detailResp); err != nil || len(detailResp.Results) == 0 {
		t.Fatalf("benchmark detail parse: %v %s", err, detailBody)
	}
	if w := ut.PerformRequest(h.Engine, "PATCH", "/api/v1/benchmarks/"+runID+"/results/"+detailResp.Results[0].ID, &ut.Body{Body: bytes.NewBuffer([]byte(`{"feedback":"agree with the call"}`)), Len: len(`{"feedback":"agree with the call"}`)}, ut.Header{Key: "Authorization", Value: "Bearer k7"}, ut.Header{Key: "Content-Type", Value: "application/json"}); w.Code != 200 {
		t.Fatalf("feedback: %d", w.Code)
	}

	// Recurring template + manual trigger
	code, body = post(t, h, "/api/v1/recurring", `{"name":"daily review","question":"keep the stack?","interval_seconds":60}`, "k7")
	if code != 201 {
		t.Fatalf("create recurring: %d %s", code, body)
	}
	rcID := field(t, body, "id")
	code, _ = post(t, h, "/api/v1/recurring/"+rcID+"/run", "", "k7")
	if code != 202 {
		t.Fatalf("run recurring now: %d", code)
	}
	code, body = get(t, h, "/api/v1/recurring", "k7")
	if code != 200 || !strings.Contains(body, "daily review") {
		t.Fatalf("list recurring: %d %s", code, body)
	}

	// Memory search (historical decisions)
	_, mbody2 := get(t, h, "/api/v1/memory?q=stack", "k7")
	if !strings.Contains(mbody2, "keep the stack?") || !strings.Contains(mbody2, `"Resolution":"approve"`) {
		t.Fatalf("memory search: %s", mbody2)
	}

	// Ask MAGI (conversational decision): returns 202 + case id; the run is
	// executed through the governed async runner.
	code, body = post(t, h, "/api/v1/assistant", `{"message":"Should we migrate the database?","background":"scale concerns"}`, "k7")
	if code != 202 || !strings.Contains(body, `"id":"case-`) {
		t.Fatalf("ask async: %d %s", code, body)
	}
	// The async run executes on the run manager goroutine; wait for it to
	// finish before asserting aggregates so the counts are deterministic.
	askID := field(t, body, "id")
	waitFor(t, h, "/api/v1/cases/"+askID, "k7", "status", "RESOLVED")

	// Admin usage (admin role token)
	code, body = get(t, h, "/api/v1/admin/usage", "k7")
	if code != 200 || !strings.Contains(body, `"total_cases":5`) || !strings.Contains(body, `"total_runs":5`) || !strings.Contains(body, `"total_tokens":500`) {
		t.Fatalf("admin usage: %d %s", code, body)
	}
}
