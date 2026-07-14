package evidence_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

func setupGateLedger(t *testing.T) (*evidence.EvidenceLedger, string) {
	t.Helper()
	l := evidence.NewEvidenceLedger("c1", "r1", "balthasar")
	l.Record("tc1", "risk_tool", "plugin", "", "high risk found", entity.ReliabilityScore{Final: 0.9})
	evs := l.List()
	if len(evs) == 0 {
		t.Fatal("no evidence recorded")
	}
	return l, evs[0].ID
}

func TestGate_CustomRuleWorstCaseClaim_FailsWithout(t *testing.T) {
	ledger, evID := setupGateLedger(t)
	summary := &entity.EvidenceSummary{
		EvidenceByType: map[string][]string{"risk": {evID}},
		Claims:         []entity.EvidenceSummaryClaim{{Statement: "the system is generally stable", Supports: []string{evID}}},
	}
	standard := entity.EvidenceStandard{
		MinEvidenceCount: 1,
		CustomRules:      []entity.EvidenceRule{{Code: "worst_case_claim_required"}},
	}
	g := evidence.NewEvidenceGate()
	res := g.Evaluate(summary, ledger, standard, "balthasar")
	if res.Passed {
		t.Fatal("expected gate to fail: missing worst-case claim")
	}
	found := false
	for _, v := range res.Violations {
		if v.Code == "WORST_CASE_CLAIM_MISSING" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected WORST_CASE_CLAIM_MISSING violation, got: %+v", res.Violations)
	}
}

func TestGate_CustomRuleWorstCaseClaim_PassesWith(t *testing.T) {
	ledger, evID := setupGateLedger(t)
	summary := &entity.EvidenceSummary{
		EvidenceByType: map[string][]string{"risk": {evID}},
		Claims:         []entity.EvidenceSummaryClaim{{Statement: "worst-case scenario: total data loss", Supports: []string{evID}}},
	}
	standard := entity.EvidenceStandard{
		MinEvidenceCount: 1,
		CustomRules:      []entity.EvidenceRule{{Code: "worst_case_claim_required"}},
	}
	g := evidence.NewEvidenceGate()
	res := g.Evaluate(summary, ledger, standard, "balthasar")
	if !res.Passed {
		t.Fatalf("expected gate to pass: %+v", res.Violations)
	}
}

func TestGate_CustomRuleUnknownCode(t *testing.T) {
	ledger, evID := setupGateLedger(t)
	summary := &entity.EvidenceSummary{
		EvidenceByType: map[string][]string{"risk": {evID}},
		Claims:         []entity.EvidenceSummaryClaim{{Statement: "any claim", Supports: []string{evID}}},
	}
	standard := entity.EvidenceStandard{
		MinEvidenceCount: 1,
		CustomRules:      []entity.EvidenceRule{{Code: "nonexistent_rule_xyz"}},
	}
	g := evidence.NewEvidenceGate()
	res := g.Evaluate(summary, ledger, standard, "balthasar")
	if res.Passed {
		t.Fatal("expected gate to fail on unknown rule code")
	}
}

func TestGate_AllCustomRulesPass(t *testing.T) {
	ledger, evID := setupGateLedger(t)
	summary := &entity.EvidenceSummary{
		EvidenceByType: map[string][]string{"risk": {evID}, "trend": {evID}},
		Claims: []entity.EvidenceSummaryClaim{
			{Statement: "this has an opportunity cost", Supports: []string{evID}},
		},
	}
	standard := entity.EvidenceStandard{
		MinEvidenceCount: 1,
		CustomRules: []entity.EvidenceRule{
			{Code: "opportunity_cost_claim"},
		},
	}
	g := evidence.NewEvidenceGate()
	res := g.Evaluate(summary, ledger, standard, "casper")
	if !res.Passed {
		t.Fatalf("expected gate to pass: %+v", res.Violations)
	}
}

func TestGate_EmptyLedgerSkipsAuthenticityChecks(t *testing.T) {
	// No-tools mode: ledger is empty, but the LLM may cite fabricated EV-IDs.
	// The gate must NOT emit EVIDENCE_NOT_FOUND / CLAIM_UNSUPPORTED then.
	emptyLedger := evidence.NewEvidenceLedger("c1", "r1", "melchior")
	summary := &entity.EvidenceSummary{
		EvidenceByType: map[string][]string{"quantitative": {"FAKE-EV-1"}},
		Claims:         []entity.EvidenceSummaryClaim{{Statement: "a claim", Supports: []string{"FAKE-EV-1"}}},
	}
	standard := entity.EvidenceStandard{MinEvidenceCount: 0}
	g := evidence.NewEvidenceGate()
	res := g.Evaluate(summary, emptyLedger, standard, "melchior")
	for _, v := range res.Violations {
		if v.Code == "EVIDENCE_NOT_FOUND" || v.Code == "CLAIM_UNSUPPORTED" {
			t.Fatalf("authenticity check should be skipped on empty ledger, got %s: %s", v.Code, v.Message)
		}
	}
}
