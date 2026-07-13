package runtime

import "github.com/jamespud/magi/backend/domain/entity"

type terminationState struct {
	gateFail       int
	consecToolFail int
	tokenUsed      int64
}

// checkTermination returns true if the loop should terminate, setting status+err.
func checkTermination(ts *terminationState, policy entity.LoopPolicy, status *LoopStatus, err *error) bool {
	if policy.MaxGateFailures > 0 && ts.gateFail >= policy.MaxGateFailures {
		*status = LoopStatusGateFailed
		return true
	}
	if policy.MaxConsecutiveToolFailures > 0 && ts.consecToolFail >= policy.MaxConsecutiveToolFailures {
		*status = LoopStatusToolFailures
		return true
	}
	if policy.TokenBudget > 0 && ts.tokenUsed >= int64(policy.TokenBudget) {
		*status = LoopStatusTokenBudget
		return true
	}
	return false
}
