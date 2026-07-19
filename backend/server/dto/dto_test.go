package dto

import (
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestFromCase(t *testing.T) {
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
	got := FromCase(c)

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
	if got.CreatedAt != now.Format(time.RFC3339) {
		t.Fatalf("CreatedAt: got %q", got.CreatedAt)
	}
	if got.UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("UpdatedAt: got %q", got.UpdatedAt)
	}
}

func TestFromResolution(t *testing.T) {
	r := &entity.Resolution{FinalDecision: entity.VoteDecisionApprove}
	r.Consensus.Round = 3
	got := FromResolution(r)
	if got.FinalDecision != "approve" || got.Round != 3 {
		t.Fatalf("FromResolution: %+v", got)
	}
}

func TestFromEvent(t *testing.T) {
	ts := time.Now()
	code := entity.MagiCode("melchior")
	e := &entity.MagiEvent{Type: entity.EventVoteSubmitted, AgentCode: &code, RunID: "r1", Timestamp: ts}
	got := FromEvent(e)
	if got.Type != "VOTE_SUBMITTED" || got.AgentCode != "melchior" || got.RunID != "r1" {
		t.Fatalf("FromEvent: %+v", got)
	}
}

func TestFromTool(t *testing.T) {
	d := port.ToolDefinition{Name: "calc", Desc: "add"}
	got := FromTool(d)
	if got.Name != "calc" || got.Desc != "add" {
		t.Fatalf("FromTool: %+v", got)
	}
}
