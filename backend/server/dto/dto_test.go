package dto

import (
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestFromCase(t *testing.T) {
	c := &entity.DecisionCase{ID: "c1", Question: "q", Status: entity.CaseStatusDraft}
	got := FromCase(c)
	if got.ID != "c1" || got.Question != "q" || got.Status != "DRAFT" {
		t.Fatalf("FromCase: %+v", got)
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
