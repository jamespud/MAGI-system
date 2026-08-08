package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestFromCase_NilResolution(t *testing.T) {
	now := time.Now()
	c := &entity.DecisionCase{
		ID:          "c1",
		Question:    "Should we adopt Rust?",
		Context:     "Java backend team of 5",
		Constraints: []entity.Constraint{{Key: "Budget", Value: "3 months", Hard: false}},
		Status:      entity.CaseStatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	got := FromCase(c, nil)

	if got.ID != "c1" {
		t.Fatalf("ID: got %q", got.ID)
	}
	if got.Question != "Should we adopt Rust?" {
		t.Fatalf("Question: got %q", got.Question)
	}
	if got.Background != "Java backend team of 5" {
		t.Fatalf("Background: got %q", got.Background)
	}
	if len(got.Constraints) != 1 || got.Constraints[0].Label != "Budget" || got.Constraints[0].Value != "3 months" {
		t.Fatalf("Constraints: got %+v", got.Constraints)
	}
	if got.Status != "DRAFT" {
		t.Fatalf("Status: got %q", got.Status)
	}
	if got.Consensus != nil {
		t.Fatalf("Consensus should be nil without resolution, got %+v", got.Consensus)
	}
	if got.Round != 0 {
		t.Fatalf("Round should be 0 without resolution, got %d", got.Round)
	}
	if got.CreatedAt != now.Format(time.RFC3339) {
		t.Fatalf("CreatedAt: got %q", got.CreatedAt)
	}
}

func TestFromCase_WithResolutionSetsConsensusAndRound(t *testing.T) {
	now := time.Now()
	c := &entity.DecisionCase{ID: "c1", Status: entity.CaseStatusResolved, CreatedAt: now, UpdatedAt: now}
	res := &entity.Resolution{
		FinalDecision: entity.VoteDecisionApprove,
		Consensus: entity.ConsensusResult{
			Outcome: entity.ConsensusStrongApproval, Round: 2,
			Votes: []entity.Vote{
				{Decision: entity.VoteDecisionApprove, Confidence: 90},
				{Decision: entity.VoteDecisionApprove, Confidence: 80},
				{Decision: entity.VoteDecisionAbstain, Confidence: 50},
			},
		},
	}
	out := FromCase(c, res)
	if out.Round != 2 {
		t.Fatalf("round: %d", out.Round)
	}
	if out.Consensus == nil || out.Consensus.Approve != 2 || out.Consensus.Abstain != 1 {
		t.Fatalf("consensus: %+v", out.Consensus)
	}
	if out.FinalDecision != "approve" {
		t.Fatalf("final decision: %s", out.FinalDecision)
	}
	if out.Confidence == 0 {
		t.Fatal("confidence should be averaged from votes, got 0")
	}
}

func TestFromEvent_DerivesMessageAndPassesPayload(t *testing.T) {
	ts := time.Now()
	code := entity.MagiCode("melchior")
	ev := &entity.MagiEvent{ID: "e1", Type: entity.EventVoteSubmitted, AgentCode: &code, RunID: "r1", Timestamp: ts, Payload: json.RawMessage(`{"round":1}`)}
	out := FromEvent(ev)
	if out.ID != "e1" {
		t.Fatalf("id: %s", out.ID)
	}
	if out.Type != "VOTE_SUBMITTED" || out.AgentCode != "melchior" || out.RunID != "r1" {
		t.Fatalf("FromEvent: %+v", out)
	}
	if out.Message == "" {
		t.Fatal("message should be derived, not empty")
	}
	if string(out.Payload) != `{"round":1}` {
		t.Fatalf("payload passthrough: %s", string(out.Payload))
	}
}

func TestFromEvent_UnknownTypeHasMessage(t *testing.T) {
	ev := &entity.MagiEvent{ID: "e2", Type: "SOMETHING_NEW"}
	out := FromEvent(ev)
	if out.Message == "" {
		t.Fatal("unknown event type should still have a message (the type itself)")
	}
}

func TestFromTool(t *testing.T) {
	d := port.ToolDefinition{Name: "calc", Desc: "add"}
	got := FromTool(d)
	if got.Name != "calc" || got.Desc != "add" {
		t.Fatalf("FromTool: %+v", got)
	}
}

func TestFromVote_NormalizesConfidence(t *testing.T) {
	v := &entity.Vote{ID: "v1", Decision: entity.VoteDecisionReject, Confidence: 0.95}
	out := FromVote(v, "melchior")
	if out.Confidence != 95 {
		t.Fatalf("FromVote confidence: got %v want 95", out.Confidence)
	}
}

func TestAvgConfidence_NormalizesLegacyVotes(t *testing.T) {
	votes := []entity.Vote{
		{Confidence: 95},
		{Confidence: 0.95},
		{Confidence: 0.94},
	}
	// (95 + 95 + 94) / 3, not the raw mixed-scale average 32.3.
	if got := avgConfidence(votes); got != 94.66666666666667 {
		t.Fatalf("avgConfidence: got %v want 94.66666666666667", got)
	}
}
