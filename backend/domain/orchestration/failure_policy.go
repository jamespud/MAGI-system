package orchestration

import (
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type FailurePolicy struct {
	Mode string // "abstain_on_fail" (default) | "fail_case"
}

func DefaultFailurePolicy() FailurePolicy { return FailurePolicy{Mode: "abstain_on_fail"} }

// HandleFailure produces an ABSTAIN vote for a failed Magi.
func (p FailurePolicy) HandleFailure(result *runtime.LoopResult, cfg *entity.MagiConfig) *entity.Vote {
	if result != nil && result.Vote != nil && result.Err == nil {
		return result.Vote
	}
	reason := "agent completed"
	if result != nil && result.Err != nil {
		reason = result.Err.Error()
	}
	return &entity.Vote{
		Decision:         entity.VoteDecisionAbstain,
		Confidence:       0,
		ReasoningSummary: "agent failed: " + reason,
	}
}
