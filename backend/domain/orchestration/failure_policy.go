package orchestration

import (
	"errors"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type FailurePolicy struct {
	Mode       string // "abstain_on_fail" (default) | "fail_case"
	RetryLimit int    // agent-level re-dispatch attempts before the policy applies (default 1)
}

func DefaultFailurePolicy() FailurePolicy {
	return FailurePolicy{Mode: "abstain_on_fail", RetryLimit: 1}
}

// ErrAgentFailed aborts the case when Mode == "fail_case": any agent failure
// fails the whole decision instead of silently converting to an abstention.
var ErrAgentFailed = errors.New("agent failed: case aborted by failure policy")

// HandleFailure produces an ABSTAIN vote for a failed Magi.
func (p FailurePolicy) HandleFailure(result *runtime.LoopResult, cfg *entity.MagiConfig) *entity.Vote {
	if result != nil && result.Vote != nil && result.Err == nil {
		return result.Vote
	}
	reason := runtime.LoopFailureReason(result)
	return &entity.Vote{
		Decision:         entity.VoteDecisionAbstain,
		Confidence:       0,
		ReasoningSummary: "agent failed: " + reason,
	}
}
