package runtime_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

type fakeApprovalRepo struct {
	mu       sync.Mutex
	reqs     map[string]*entity.ApprovalRequest
	onCreate func(a *entity.ApprovalRequest)
}

func newFakeApprovalRepo() *fakeApprovalRepo {
	return &fakeApprovalRepo{reqs: make(map[string]*entity.ApprovalRequest)}
}

func (r *fakeApprovalRepo) Create(_ context.Context, a *entity.ApprovalRequest) error {
	r.mu.Lock()
	if a.ID == "" {
		a.ID = "appr-fake"
	}
	r.reqs[a.ID] = a
	cb := r.onCreate
	r.mu.Unlock()
	if cb != nil {
		cb(a)
	}
	return nil
}

func (r *fakeApprovalRepo) Get(_ context.Context, id string) (*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (r *fakeApprovalRepo) FindByKey(_ context.Context, caseID, runID, toolName string) (*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.reqs {
		if a.CaseID == caseID && a.RunID == runID && a.ToolName == toolName {
			return a, nil
		}
	}
	return nil, nil
}

func (r *fakeApprovalRepo) List(_ context.Context, _ string) ([]*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*entity.ApprovalRequest, 0, len(r.reqs))
	for _, a := range r.reqs {
		out = append(out, a)
	}
	return out, nil
}

func (r *fakeApprovalRepo) ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error) {
	return r.List(ctx, "")
}

func (r *fakeApprovalRepo) decide(id string, status entity.ApprovalStatus, by, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok || a.Status != entity.ApprovalPending {
		return nil
	}
	a.Status = status
	a.DecidedBy = by
	a.Reason = reason
	return nil
}

func (r *fakeApprovalRepo) Approve(_ context.Context, id, by, reason string) error {
	return r.decide(id, entity.ApprovalApproved, by, reason)
}
func (r *fakeApprovalRepo) Reject(_ context.Context, id, by, reason string) error {
	return r.decide(id, entity.ApprovalRejected, by, reason)
}
func (r *fakeApprovalRepo) MarkExpired(_ context.Context, id string) error {
	return r.decide(id, entity.ApprovalExpired, "", "")
}

func approvalTestLoop(t *testing.T, responses []*schema.Message, pol *toolpolicy.Policy, repo port.ApprovalRepository, rec *recordingEventPub) *runtime.AgentLoop {
	t.Helper()
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: responses}},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{{
			Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding,
		}}},
		ToolExec: &stubToolExec{}, Validator: v, Gen: gen, EventPub: rec,
		ToolPolicy: pol, ApprovalRepo: repo,
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}
	return loop
}

func TestAgentLoop_ApprovalApprovedExecutesTool(t *testing.T) {
	repo := newFakeApprovalRepo()
	repo.onCreate = func(a *entity.ApprovalRequest) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = repo.Approve(context.Background(), a.ID, "human-1", "ok")
		}()
	}
	rec := &recordingEventPub{}
	loop := approvalTestLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, toolpolicy.NewPolicy([]string{"calc"}, nil), repo, rec)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.ApprovalTimeout = 2 * time.Second
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", RunID: "run-1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote")
	}
	var executed *runtime.ToolCallRecord
	for _, st := range res.Trace.Steps {
		for i := range st.ToolCalls {
			tc := &st.ToolCalls[i]
			if tc.ToolName == "calc" && tc.Valid {
				executed = tc
			}
		}
	}
	if executed == nil || executed.ApprovedBy != "human-1" {
		t.Fatalf("expected executed approved tool call: %+v", executed)
	}
	req, _ := repo.FindByKey(context.Background(), "c1", "run-1", "calc")
	if req == nil || req.Status != entity.ApprovalApproved {
		t.Fatalf("request: %+v", req)
	}
}

func TestAgentLoop_ApprovalRejectedFeedsBack(t *testing.T) {
	repo := newFakeApprovalRepo()
	repo.onCreate = func(a *entity.ApprovalRequest) {
		_ = repo.Reject(context.Background(), a.ID, "human-2", "too risky")
	}
	rec := &recordingEventPub{}
	loop := approvalTestLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, toolpolicy.NewPolicy([]string{"calc"}, nil), repo, rec)
	cfg := evidenceCfg(1, 0)
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", RunID: "run-2", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after rejection feedback")
	}
	var rejected *runtime.ToolCallRecord
	for _, st := range res.Trace.Steps {
		for i := range st.ToolCalls {
			tc := &st.ToolCalls[i]
			if tc.ToolName == "calc" && tc.Err != "" {
				rejected = tc
			}
		}
	}
	if rejected == nil || !contains(rejected.Err, "rejected by human") {
		t.Fatalf("expected rejected tool call: %+v", rejected)
	}
	req, _ := repo.FindByKey(context.Background(), "c1", "run-2", "calc")
	if req == nil || req.Status != entity.ApprovalRejected {
		t.Fatalf("request: %+v", req)
	}
}

func TestAgentLoop_ApprovalExpires(t *testing.T) {
	repo := newFakeApprovalRepo() // stays pending forever
	rec := &recordingEventPub{}
	loop := approvalTestLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, toolpolicy.NewPolicy([]string{"calc"}, nil), repo, rec)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.ApprovalTimeout = 50 * time.Millisecond
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", RunID: "run-3", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after timeout")
	}
	req, _ := repo.FindByKey(context.Background(), "c1", "run-3", "calc")
	if req == nil || req.Status != entity.ApprovalExpired {
		t.Fatalf("request: %+v", req)
	}
}

func TestAgentLoop_ContextCompactionTriggersSummary(t *testing.T) {
	rec := &recordingEventPub{}
	loop := approvalTestLoop(t, []*schema.Message{
		withUsage(callMsg("c1", "calc", `{"a":1,"b":2}`), 0, 0, 60),
		withUsage(callMsg("c1", "calc", `{"a":1,"b":2}`), 0, 0, 60),
		finalMsg("summary of the working memory"),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil, nil, rec)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.TokenBudget = 200
	cfg.LoopPolicy.TokenCompactionThreshold = 0.5
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", RunID: "run-4", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after compaction")
	}
	found := false
	for _, e := range rec.events {
		if e.Type == entity.EventContextCompacted {
			found = true
		}
	}
	if !found {
		t.Fatal("expected CONTEXT_COMPACTED event")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
