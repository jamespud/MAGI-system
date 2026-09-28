package orchestration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// Issue #10: the voting entry point must classify every non-vote as an absence
// and never fabricate a ballot. The kinds stay distinguishable so diagnostics
// can tell an agent failure from a timeout, a cancellation, a missing ballot and
// a malformed one.
func TestFailurePolicy_ClassifySeparatesBallotsFromAbsences(t *testing.T) {
	policy := orchestration.DefaultFailurePolicy()
	abstain := &entity.Vote{Decision: entity.VoteDecisionAbstain}

	cases := []struct {
		name       string
		result     *runtime.LoopResult
		wantBallot bool
		wantKind   entity.AbsenceKind
	}{
		{name: "genuine abstention is a ballot", result: &runtime.LoopResult{Status: runtime.LoopStatusCompleted, Vote: abstain}, wantBallot: true},
		{name: "nil result", result: nil, wantKind: entity.AbsenceMissing},
		{name: "agent error", result: &runtime.LoopResult{Status: runtime.LoopStatusError, Err: errors.New("boom")}, wantKind: entity.AbsenceAgentFailed},
		{name: "deadline exceeded", result: &runtime.LoopResult{Status: runtime.LoopStatusError, Err: context.DeadlineExceeded}, wantKind: entity.AbsenceTimeout},
		{name: "cancelled", result: &runtime.LoopResult{Status: runtime.LoopStatusCancelled}, wantKind: entity.AbsenceCancelled},
		{name: "completed without a ballot", result: &runtime.LoopResult{Status: runtime.LoopStatusCompleted}, wantKind: entity.AbsenceMissing},
		{name: "empty decision", result: &runtime.LoopResult{Status: runtime.LoopStatusCompleted, Vote: &entity.Vote{}}, wantKind: entity.AbsenceInvalid},
		{name: "unknown decision", result: &runtime.LoopResult{Status: runtime.LoopStatusCompleted, Vote: &entity.Vote{Decision: "maybe"}}, wantKind: entity.AbsenceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vote, absence := policy.Classify(tc.result)
			if tc.wantBallot {
				if vote == nil || absence != nil {
					t.Fatalf("want a ballot, got vote=%+v absence=%+v", vote, absence)
				}
				return
			}
			if vote != nil {
				t.Fatalf("a non-vote must never be returned as a ballot: %+v", vote)
			}
			if absence == nil || absence.Kind != tc.wantKind {
				t.Fatalf("absence=%+v want kind %q", absence, tc.wantKind)
			}
		})
	}
}
