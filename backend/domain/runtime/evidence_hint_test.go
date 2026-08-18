package runtime_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// TestAvailableEvidenceHint guards the gate-failure feedback loop: when the
// evidence gate rejects a summary for citing unknown EV-IDs, the model must
// be told which EV-IDs actually exist so it can fix the citation instead of
// re-fabricating IDs.
func TestAvailableEvidenceHint(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c1", "r1", "melchior")
	ledger.Record("tc1", "web_search", "local", "u1", "obs1", entity.ReliabilityScore{Final: 0.9})
	ledger.Record("tc2", "get_quote", "mcp", "u2", "obs2", entity.ReliabilityScore{Final: 0.75})

	hint := runtime.AvailableEvidenceHint(ledger, 10)
	if hint == "" {
		t.Fatal("expected a non-empty hint when evidence exists")
	}
	for _, want := range []string{"Available EV-IDs", "EV-001", "EV-002", "web_search", "get_quote", "0.90", "0.75"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}

	if got := runtime.AvailableEvidenceHint(evidence.NewEvidenceLedger("c", "r", "m"), 10); got != "" {
		t.Fatalf("expected empty hint for empty ledger, got %q", got)
	}
}

// TestAvailableEvidenceHint_Truncated guards against unbounded prompts: with
// more evidence than the hint limit, the hint must stop listing.
func TestAvailableEvidenceHint_Truncated(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c1", "r1", "melchior")
	for i := 0; i < 12; i++ {
		ledger.Record("t", "tool", "local", "u", "obs", entity.ReliabilityScore{Final: 0.8})
	}
	hint := runtime.AvailableEvidenceHint(ledger, 5)
	if strings.Count(hint, "EV-0") != 5 {
		t.Fatalf("expected 5 EV-IDs listed, got %q", hint)
	}
	if !strings.Contains(hint, "…") {
		t.Fatalf("expected truncation marker in %q", hint)
	}
}

// TestBuildClaimFeedback guards claim-submission feedback: claims citing
// unknown EV-IDs must be explicitly rejected with the available-ID hint
// instead of being silently dropped while the model is told "Claims recorded".
func TestBuildClaimFeedback(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c1", "r1", "melchior")
	ledger.Record("tc1", "web_search", "local", "u1", "obs1", entity.ReliabilityScore{Final: 0.9})

	good := []entity.EvidenceSummaryClaim{{Statement: "s1", Supports: []string{"EV-001"}}}
	if got := runtime.BuildClaimFeedback(good, ledger); !strings.Contains(got, "Claims recorded") {
		t.Fatalf("valid claim feedback: %q", got)
	}

	bad := []entity.EvidenceSummaryClaim{{Statement: "s1", Supports: []string{"EV-999"}}}
	got := runtime.BuildClaimFeedback(bad, ledger)
	if strings.Contains(got, "Claims recorded") {
		t.Fatalf("invalid claim must not say recorded: %q", got)
	}
	if !strings.Contains(got, "EV-999") || !strings.Contains(got, "Available EV-IDs") || !strings.Contains(got, "EV-001") {
		t.Fatalf("invalid claim feedback missing detail: %q", got)
	}

	empty := runtime.BuildClaimFeedback(bad, evidence.NewEvidenceLedger("c", "r", "m"))
	if strings.Contains(empty, "Available EV-IDs") {
		t.Fatalf("empty ledger must not produce a hint: %q", empty)
	}
}
