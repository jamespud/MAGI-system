package runtime_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// TestCheckTermination_SetsErr guards the failure-reason transparency fix:
// every deterministic termination must populate Err so agent_run.err and
// ABSTAIN reasoning carry the real cause instead of "agent completed".
func TestCheckTermination_SetsErr(t *testing.T) {
	policy := entity.LoopPolicy{
		MaxGateFailures:                   3,
		MaxConsecutiveToolFailures:        5,
		MaxConsecutiveValidationFailures:  5,
		TokenBudget:                       1000,
	}
	cases := []struct {
		name string
		ts   runtime.TerminationState
		want string
	}{
		{"gate", runtime.TerminationState{GateFail: 3}, "evidence gate failed"},
		{"tool", runtime.TerminationState{ConsecToolFail: 5}, "tool failures"},
		{"validation", runtime.TerminationState{ValidationFail: 5}, "invalid responses"},
		{"budget", runtime.TerminationState{TokenUsed: 1000}, "token budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var status runtime.LoopStatus
			var err error
			if !runtime.CheckTermination(&tc.ts, policy, &status, &err) {
				t.Fatal("expected termination")
			}
			if err == nil {
				t.Fatalf("expected Err for %s termination, got nil", tc.name)
			}
			if len(err.Error()) < 5 {
				t.Fatalf("uninformative error: %q", err.Error())
			}
		})
	}
}
