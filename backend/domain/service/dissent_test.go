package service_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/service"
)

func TestExtractDissent_ReturnsMinorityPositions(t *testing.T) {
	votes := []*entity.Vote{
		{AgentRunID: "r-mel", Decision: entity.VoteDecisionApprove, ReasoningSummary: "correct", EvidenceIDs: []string{"EV-1"}, KeyClaimIDs: []string{"CL-1"}},
		{AgentRunID: "r-bal", Decision: entity.VoteDecisionReject, ReasoningSummary: "too risky", EvidenceIDs: []string{"EV-2"}, Conditions: []entity.DecisionCondition{{Statement: "must rollback", MustHold: true}}},
		{AgentRunID: "r-cas", Decision: entity.VoteDecisionAbstain, ReasoningSummary: "unsure"},
	}
	codes := map[string]entity.MagiCode{"r-mel": "melchior", "r-bal": "balthasar", "r-cas": "casper"}
	out := service.ExtractDissent(entity.VoteDecisionApprove, votes, codes)
	if len(out) != 1 {
		t.Fatalf("want 1 dissenter, got %d", len(out))
	}
	d := out[0]
	if d.AgentCode != "balthasar" || d.Decision != entity.VoteDecisionReject || d.Reasoning != "too risky" {
		t.Fatalf("dissent: %+v", d)
	}
	if len(d.EvidenceIDs) != 1 || d.EvidenceIDs[0] != "EV-2" || len(d.Conditions) != 1 {
		t.Fatalf("dissent details: %+v", d)
	}
}

func TestExtractDissent_EmptyForUnanimous(t *testing.T) {
	votes := []*entity.Vote{{AgentRunID: "r1", Decision: entity.VoteDecisionApprove}}
	if out := service.ExtractDissent(entity.VoteDecisionApprove, votes, map[string]entity.MagiCode{"r1": "melchior"}); len(out) != 0 {
		t.Fatalf("want no dissent, got %+v", out)
	}
}

func TestExtractDissent_SkipsAbstain(t *testing.T) {
	votes := []*entity.Vote{
		{AgentRunID: "r1", Decision: entity.VoteDecisionAbstain},
		{AgentRunID: "r2", Decision: entity.VoteDecisionApprove},
	}
	if out := service.ExtractDissent(entity.VoteDecisionApprove, votes, nil); len(out) != 0 {
		t.Fatalf("abstain is not dissent: %+v", out)
	}
}
