package runtime

import (
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

// TerminationState tracks the deterministic loop-termination counters. It is
// exported for tests; production code mutates it via the agent loop.
type TerminationState struct {
	GateFail       int
	ConsecToolFail int
	TokenUsed      int64
	ValidationFail int
	ToolCalls      int
}

// CheckTermination returns true if the loop should terminate, setting status
// AND a concrete error so the failure is observable downstream.
func CheckTermination(ts *TerminationState, policy entity.LoopPolicy, status *LoopStatus, err *error) bool {
	if policy.MaxGateFailures > 0 && ts.GateFail >= policy.MaxGateFailures {
		*status = LoopStatusGateFailed
		*err = fmt.Errorf("evidence gate failed after %d attempts", ts.GateFail)
		return true
	}
	if policy.MaxConsecutiveToolFailures > 0 && ts.ConsecToolFail >= policy.MaxConsecutiveToolFailures {
		*status = LoopStatusToolFailures
		*err = fmt.Errorf("%d consecutive tool failures", ts.ConsecToolFail)
		return true
	}
	if policy.TokenBudget > 0 && ts.TokenUsed >= int64(policy.TokenBudget) {
		*status = LoopStatusTokenBudget
		*err = fmt.Errorf("token budget exceeded (%d tokens)", ts.TokenUsed)
		return true
	}
	if policy.MaxConsecutiveValidationFailures > 0 && ts.ValidationFail >= policy.MaxConsecutiveValidationFailures {
		*status = LoopStatusValidationFailed
		*err = fmt.Errorf("%d consecutive invalid responses", ts.ValidationFail)
		return true
	}
	return false
}

// LoopFailureReason derives a human-readable failure reason from a LoopResult
// even when Err is nil (deterministic terminations set Status only). It is
// used by the failure policy so ABSTAIN votes and agent_run.err carry the real
// cause instead of a misleading "agent completed".
func LoopFailureReason(r *LoopResult) string {
	if r == nil {
		return "no result"
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	switch r.Status {
	case LoopStatusGateFailed:
		return "evidence gate failed"
	case LoopStatusToolFailures:
		return "tool failures"
	case LoopStatusValidationFailed:
		return "invalid responses"
	case LoopStatusTokenBudget:
		return "token budget exceeded"
	case LoopStatusMaxSteps:
		return "max steps exceeded"
	case LoopStatusError:
		return "agent error"
	case LoopStatusCancelled:
		return "cancelled"
	default:
		return "agent completed"
	}
}
