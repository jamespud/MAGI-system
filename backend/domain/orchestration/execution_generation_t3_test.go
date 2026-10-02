package orchestration

import "testing"

// T3 / #21: persisted artifact IDs use the durable Case execution generation,
// never the resettable ExecutionAttempt retry ordinal.
func TestExecutionRunIDUsesGenerationNamespace(t *testing.T) {
	got := executionRunID("case-1", "melchior", 7, 2, "investigate")
	want := "case-1-melchior-g7-r2-investigate"
	if got != want {
		t.Fatalf("executionRunID = %q, want %q", got, want)
	}
}
