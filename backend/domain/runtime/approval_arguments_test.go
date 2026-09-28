package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// Issue #9: an approval decision authorizes one exact invocation, not a tool
// name. Approving {"a":1,"b":2} must not authorize a later call to the same
// tool with {"a":9,"b":9} inside the same case/run.
func TestAgentLoop_ApprovalDoesNotReuseAcrossDifferentArguments(t *testing.T) {
	repo := newFakeApprovalRepo()
	repo.onCreate = func(a *entity.ApprovalRequest) {
		if a.Arguments != `{"a":1,"b":2}` {
			return
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = repo.Approve(context.Background(), a.ID, "human-1", "ok")
		}()
	}
	rec := &recordingEventPub{}
	loop := approvalTestLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		callMsg("c2", "calc", `{"a":9,"b":9}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, toolpolicy.NewPolicy([]string{"calc"}, nil), repo, rec)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.ApprovalTimeout = 200 * time.Millisecond

	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{
		CaseID: "c1", RunID: "run-args", Task: entity.DecisionTask{CanonicalQuestion: "compute"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var second *runtime.ToolCallRecord
	for _, st := range res.Trace.Steps {
		for i := range st.ToolCalls {
			if tc := &st.ToolCalls[i]; tc.ToolCallID == "c2" {
				second = tc
			}
		}
	}
	if second == nil {
		t.Fatal("expected the second calc call to be recorded in the trace")
	}
	if second.ApprovedBy != "" {
		t.Fatalf("second call with different arguments reused the first approval: approved_by=%q err=%q", second.ApprovedBy, second.Err)
	}
	if second.Err == "" {
		t.Fatalf("second call with different arguments must be refused or held for a new approval, got no error")
	}
}
