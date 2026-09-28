package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

func approvalScript() []*schema.Message {
	return []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		callMsg("c2", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
}

func runWithApproval(t *testing.T, repo port.ApprovalRepository, responses []*schema.Message, runID string) *runtime.LoopResult {
	t.Helper()
	loop := approvalTestLoop(t, responses, toolpolicy.NewPolicy([]string{"calc"}, nil), repo, &recordingEventPub{})
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.ApprovalTimeout = 200 * time.Millisecond
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{
		CaseID: "c1", RunID: runID, Task: entity.DecisionTask{CanonicalQuestion: "compute"},
	})
	if err != nil {
		t.Fatalf("run %s: %v", runID, err)
	}
	return res
}

// Issue #9 (follow-up): the approval identity is the logical invocation, so two
// independent calls that use the same tool and identical arguments are still
// two calls and each needs its own decision.
func TestAgentLoop_ApprovalIsScopedToTheInvocation(t *testing.T) {
	repo := newFakeApprovalRepo()
	repo.onCreate = func(a *entity.ApprovalRequest) {
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = repo.Approve(context.Background(), a.ID, "human-1", "ok")
		}()
	}

	runWithApproval(t, repo, approvalScript(), "run-inv")

	requests, err := repo.List(context.Background(), "c1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("two independent calls need two approvals, got %d", len(requests))
	}
	if requests[0].InvocationID == requests[1].InvocationID {
		t.Fatalf("both requests share invocation %q", requests[0].InvocationID)
	}
}

// The dispatcher retries a failed agent with a "-retryN" run id. That is the
// same logical call, so it must reuse the decision it already has instead of
// asking the human again.
func TestAgentLoop_ApprovalIsReusedAcrossDispatcherRetries(t *testing.T) {
	repo := newFakeApprovalRepo()
	approvals := 0
	repo.onCreate = func(a *entity.ApprovalRequest) {
		approvals++
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = repo.Approve(context.Background(), a.ID, "human-1", "ok")
		}()
	}

	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	runWithApproval(t, repo, responses, "run-retry")
	runWithApproval(t, repo, responses, "run-retry-retry1")

	if approvals != 1 {
		t.Fatalf("a dispatcher retry must reuse the approval, got %d requests", approvals)
	}
}

// racingApprovalRepo simulates losing the create race: the caller's lookup
// misses, its insert conflicts because another writer already stored the
// authoritative request, and the follow-up lookup returns that request.
type racingApprovalRepo struct {
	*fakeApprovalRepo
	rival *entity.ApprovalRequest
}

func (r *racingApprovalRepo) FindByInvocation(_ context.Context, caseID, invocationID string) (*entity.ApprovalRequest, error) {
	if r.rival != nil && r.rival.CaseID == caseID && r.rival.InvocationID == invocationID {
		return r.rival, nil
	}
	return nil, nil
}

func (r *racingApprovalRepo) Create(_ context.Context, a *entity.ApprovalRequest) error {
	r.rival = &entity.ApprovalRequest{
		ID: "appr-rival", CaseID: a.CaseID, RunID: a.RunID, AgentCode: a.AgentCode,
		ToolName: a.ToolName, Arguments: a.Arguments, IntentDigest: a.IntentDigest,
		InvocationID: a.InvocationID, Status: entity.ApprovalApproved,
		DecidedBy: "human-rival", RequestedAt: a.RequestedAt,
	}
	return port.ErrApprovalConflict
}

func (r *racingApprovalRepo) Get(_ context.Context, id string) (*entity.ApprovalRequest, error) {
	if r.rival != nil && r.rival.ID == id {
		return r.rival, nil
	}
	return nil, nil
}

func TestAgentLoop_ApprovalConflictWaitsOnTheAuthoritativeRequest(t *testing.T) {
	repo := &racingApprovalRepo{fakeApprovalRepo: newFakeApprovalRepo()}

	res := runWithApproval(t, repo, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, "run-race")

	var executed *runtime.ToolCallRecord
	for _, st := range res.Trace.Steps {
		for i := range st.ToolCalls {
			if tc := &st.ToolCalls[i]; tc.ToolName == "calc" && tc.Err == "" {
				executed = tc
			}
		}
	}
	if executed == nil {
		t.Fatal("the call must proceed under the winning request's decision")
	}
	if executed.ApprovedBy != "human-rival" {
		t.Fatalf("expected the race winner's decision, got approved_by=%q", executed.ApprovedBy)
	}
}
