package runtime_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/entity"
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
