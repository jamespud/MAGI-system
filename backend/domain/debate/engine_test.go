package debate

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

func TestBuildPacket_MajorityMinority(t *testing.T) {
	votes := []entity.Vote{
		{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{"EV-001"}},
		{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{"EV-002"}},
		{Decision: entity.VoteDecisionReject, EvidenceIDs: []string{"EV-003"}},
	}
	claims := []*entity.Claim{
		{ID: "CL-001", Statement: "should migrate", Supports: []string{"EV-001"}, Contradicts: []string{"CL-002"}},
		{ID: "CL-002", Statement: "should not migrate", Supports: []string{"EV-003"}, Contradicts: []string{"CL-001"}},
	}
	eng := NewDebateEngine(nil)
	pkt := eng.BuildPacket(votes, claims, 1, nil)
	if len(pkt.MajorityVotes) != 2 {
		t.Fatalf("majority: %d", len(pkt.MajorityVotes))
	}
	if len(pkt.MinorityVotes) != 1 {
		t.Fatalf("minority: %d", len(pkt.MinorityVotes))
	}
	if len(pkt.ConflictingClaims) == 0 {
		t.Fatalf("expected conflicts")
	}
}

func TestBuildPacket_AllDifferent(t *testing.T) {
	votes := []entity.Vote{
		{Decision: entity.VoteDecisionApprove},
		{Decision: entity.VoteDecisionReject},
		{Decision: entity.VoteDecisionAbstain},
	}
	eng := NewDebateEngine(nil)
	pkt := eng.BuildPacket(votes, nil, 1, nil)
	if len(pkt.MajorityVotes) != 0 {
		t.Fatalf("majority should be 0, got %d", len(pkt.MajorityVotes))
	}
	if len(pkt.MinorityVotes) != 3 {
		t.Fatalf("minority should be 3, got %d", len(pkt.MinorityVotes))
	}
}

func makeLedgerWithEV(evID string) *evidence.EvidenceLedger {
	l := evidence.NewEvidenceLedger("case", "run", "melchior")
	l.Record("tc1", "calc", "local", "", "observation", entity.ReliabilityScore{Base: 0.9, Final: 0.9})
	// EV-001 is now in the ledger; rename the ID by fetching
	for _, r := range l.List() {
		_ = r
	}
	return l
}

func TestValidateReflection_ChangeWithNewEvidence(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c", "r", "m")
	ledger.Record("tc", "tool", "local", "", "obs", entity.ReliabilityScore{Final: 0.9})
	evID := ledger.List()[0].ID
	r := &entity.Reflection{PositionChange: entity.PositionChangeChange, NewEvidenceIDs: []string{evID}}
	err := ValidateReflection(r, &entity.Vote{}, ledger, map[string]bool{}, false)
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestValidateReflection_ChangeWithoutEvidence(t *testing.T) {
	r := &entity.Reflection{PositionChange: entity.PositionChangeChange}
	err := ValidateReflection(r, &entity.Vote{}, evidence.NewEvidenceLedger("c", "r", "m"), map[string]bool{}, false)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidateReflection_ChangeWithFakeEvidence(t *testing.T) {
	r := &entity.Reflection{PositionChange: entity.PositionChangeChange, NewEvidenceIDs: []string{"EV-999"}}
	err := ValidateReflection(r, &entity.Vote{}, evidence.NewEvidenceLedger("c", "r", "m"), map[string]bool{}, false)
	if err == nil {
		t.Fatalf("expected error for fake EV-ID")
	}
}

func TestValidateReflection_Maintain(t *testing.T) {
	r := &entity.Reflection{PositionChange: entity.PositionChangeMaintain}
	err := ValidateReflection(r, &entity.Vote{}, evidence.NewEvidenceLedger("c", "r", "m"), map[string]bool{}, false)
	if err != nil {
		t.Fatalf("maintain should be valid: %v", err)
	}
}

func TestInferReflection_Change(t *testing.T) {
	prev := &entity.Vote{Decision: entity.VoteDecisionReject, EvidenceIDs: []string{"EV-001"}}
	curr := &entity.Vote{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{"EV-001", "EV-002"}, AgentRunID: "run-2"}
	r := InferReflection(prev, curr, 2)
	if r == nil {
		t.Fatalf("nil reflection")
	}
	if r.PositionChange != entity.PositionChangeChange {
		t.Fatalf("position: %s", r.PositionChange)
	}
	if len(r.NewEvidenceIDs) != 1 || r.NewEvidenceIDs[0] != "EV-002" {
		t.Fatalf("new evidence: %+v", r.NewEvidenceIDs)
	}
}

func TestInferReflection_Maintain(t *testing.T) {
	prev := &entity.Vote{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{"EV-001"}}
	curr := &entity.Vote{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{"EV-001"}}
	r := InferReflection(prev, curr, 2)
	if r.PositionChange != entity.PositionChangeMaintain {
		t.Fatalf("position: %s", r.PositionChange)
	}
}

func TestBuildPacket_SynthesizesConflictsWhenNoAssertion(t *testing.T) {
	votes := []entity.Vote{
		{Decision: entity.VoteDecisionApprove, KeyClaimIDs: []string{"C-major-1"}},
		{Decision: entity.VoteDecisionApprove, KeyClaimIDs: []string{"C-major-2"}},
		{Decision: entity.VoteDecisionReject, KeyClaimIDs: []string{"C-minor-1"}},
	}
	claims := []*entity.Claim{
		{ID: "C-major-1", Statement: "rust is faster"},
		{ID: "C-major-2", Statement: "rust is safer"},
		{ID: "C-minor-1", Statement: "rewrite is risky"},
	}
	eng := NewDebateEngine(nil)
	pkt := eng.BuildPacket(votes, claims, 1, nil)
	if len(pkt.ConflictingClaims) == 0 {
		t.Fatalf("expected synthesized conflicts when no agent-asserted contradictions, got 0")
	}
	for _, c := range pkt.ConflictingClaims {
		if c.Reason != "opposing-vote" {
			t.Fatalf("synthesized conflict reason should be opposing-vote, got %q", c.Reason)
		}
	}
	if len(pkt.ConflictingClaims) > 3 {
		t.Fatalf("synthesized conflicts should be capped at 3, got %d", len(pkt.ConflictingClaims))
	}
}

func TestBuildPacket_PrefersAgentAssertedConflicts(t *testing.T) {
	votes := []entity.Vote{
		{Decision: entity.VoteDecisionApprove, KeyClaimIDs: []string{"C-major-1"}},
		{Decision: entity.VoteDecisionReject, KeyClaimIDs: []string{"C-minor-1"}},
	}
	claims := []*entity.Claim{
		{ID: "C-major-1", Statement: "a", Contradicts: []string{"C-minor-1"}},
		{ID: "C-minor-1", Statement: "b"},
	}
	eng := NewDebateEngine(nil)
	pkt := eng.BuildPacket(votes, claims, 1, nil)
	if len(pkt.ConflictingClaims) == 0 {
		t.Fatalf("expected agent-asserted conflicts")
	}
	for _, c := range pkt.ConflictingClaims {
		if c.Reason == "opposing-vote" {
			t.Fatalf("should use agent-asserted conflicts, not synthesized ones")
		}
	}
}
