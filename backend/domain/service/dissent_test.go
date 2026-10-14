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
	codes := []entity.MagiCode{"melchior", "balthasar", "casper"}
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

// A complete unanimous round records an explicit "no dissent": the result is a
// non-nil empty slice so persistence can distinguish it from "never recorded".
func TestExtractDissent_EmptyForUnanimous(t *testing.T) {
	votes := []*entity.Vote{{AgentRunID: "r1", Decision: entity.VoteDecisionApprove}}
	out := service.ExtractDissent(entity.VoteDecisionApprove, votes, []entity.MagiCode{"melchior"})
	if len(out) != 0 {
		t.Fatalf("want no dissent, got %+v", out)
	}
	if out == nil {
		t.Fatal("a complete unanimous round must record NONE (non-nil empty), not UNKNOWN (nil)")
	}
	if entity.DissentStateOf(out) != entity.DissentNone {
		t.Fatalf("state = %s, want NONE", entity.DissentStateOf(out))
	}
}

// An undecidable round (no authoritative decision) has no minority record at
// all: nil, which callers persist as UNKNOWN.
func TestExtractDissent_UnknownWhenNoFinalDecision(t *testing.T) {
	votes := []*entity.Vote{{Decision: entity.VoteDecisionReject}}
	if out := service.ExtractDissent("", votes, []entity.MagiCode{"casper"}); out != nil {
		t.Fatalf("no final decision must yield an unrecorded (nil) dissent, got %+v", out)
	}
}

func TestExtractDissent_SkipsAbstainAndMissingSlot(t *testing.T) {
	votes := []*entity.Vote{
		nil, // a participant that produced no ballot is not a dissenter
		{Decision: entity.VoteDecisionAbstain},
		{Decision: entity.VoteDecisionApprove},
	}
	if out := service.ExtractDissent(entity.VoteDecisionApprove, votes, []entity.MagiCode{"melchior", "balthasar", "casper"}); len(out) != 0 {
		t.Fatalf("abstain and missing ballots are not dissent: %+v", out)
	}
}

// Attribution follows ballot position, not a persisted run id, so a repo-less
// run (where AgentRunID is empty on every ballot) still names the right agent.
func TestExtractDissent_AttributionByPosition(t *testing.T) {
	votes := []*entity.Vote{
		{Decision: entity.VoteDecisionApprove},
		{Decision: entity.VoteDecisionReject, ReasoningSummary: "risk"},
	}
	out := service.ExtractDissent(entity.VoteDecisionApprove, votes, []entity.MagiCode{"melchior", "balthasar"})
	if len(out) != 1 || out[0].AgentCode != "balthasar" {
		t.Fatalf("want balthasar dissent, got %+v", out)
	}
}
