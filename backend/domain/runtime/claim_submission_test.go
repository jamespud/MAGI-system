package runtime_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// S7c: CLAIM_SUBMISSION -- model submits claims mid-gather, claims recorded, loop continues to vote.
func TestAgentLoop_ClaimSubmission_ValidEVIDs(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		// Claim submission with valid EV-001 (created by the tool call above)
		finalMsg(`{"type":"claim_submission","claims":[{"statement":"result is 3","supports":["EV-001"],"contradicts":[]}]}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)

	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	// Claims should be recorded in the ledger
	claims := res.Ledger.ListClaims()
	if len(claims) == 0 {
		t.Fatalf("expected claims in ledger after claim submission")
	}
	if claims[0].Statement != "result is 3" {
		t.Fatalf("claim statement: %s", claims[0].Statement)
	}
}

// S7c: CLAIM_SUBMISSION with fake EV-IDs -- claims rejected, loop continues.
func TestAgentLoop_ClaimSubmission_FakeEVIDs(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		// Claim with non-existent EV-999
		finalMsg(`{"type":"claim_submission","claims":[{"statement":"fake","supports":["EV-999"],"contradicts":[]}]}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)

	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	// Claims with fake EV-IDs should NOT be recorded
	claims := res.Ledger.ListClaims()
	if len(claims) != 0 {
		t.Fatalf("expected 0 claims (fake EV-IDs rejected), got %d", len(claims))
	}
}
