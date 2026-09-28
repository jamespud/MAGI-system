package orchestration

import (
	"context"
	"errors"
	"fmt"

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

// Classify separates an authoritative ballot from a non-vote. Exactly one of
// the two results is non-nil.
//
// A failure, timeout, cancellation, missing ballot or unknown decision value is
// an absence, never a Vote: ABSTAIN is a decision a persona makes, not the
// absence of one, and fabricating it lets a missing participant satisfy quorum.
func (p FailurePolicy) Classify(result *runtime.LoopResult) (*entity.Vote, *entity.AgentAbsence) {
	switch {
	case result == nil:
		return nil, &entity.AgentAbsence{Kind: entity.AbsenceMissing, Reason: "agent produced no result"}
	case result.Err != nil || result.Status != runtime.LoopStatusCompleted:
		return nil, &entity.AgentAbsence{Kind: absenceKind(result), Reason: runtime.LoopFailureReason(result)}
	case result.Vote == nil:
		return nil, &entity.AgentAbsence{Kind: entity.AbsenceMissing, Reason: "agent returned no final ballot"}
	case !entity.IsValidVoteDecision(result.Vote.Decision):
		return nil, &entity.AgentAbsence{
			Kind:   entity.AbsenceInvalid,
			Reason: fmt.Sprintf("invalid vote decision %q", result.Vote.Decision),
		}
	}
	return result.Vote, nil
}

// absenceKind classifies why no ballot arrived. A timeout is a distinct
// operational fact from a generic failure or an explicit cancellation.
func absenceKind(result *runtime.LoopResult) entity.AbsenceKind {
	switch {
	case errors.Is(result.Err, context.DeadlineExceeded):
		return entity.AbsenceTimeout
	case result.Status == runtime.LoopStatusCancelled:
		return entity.AbsenceCancelled
	default:
		return entity.AbsenceAgentFailed
	}
}
