package consensus

import (
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

type ConsensusEngine struct{}

func NewConsensusEngine() *ConsensusEngine { return &ConsensusEngine{} }

// Evaluate counts votes and classifies the outcome deterministically (ADR-009).
// It does NOT decide state transitions (debate vs resolve); the orchestrator
// inspects the Outcome + Detail + policy to choose the next state.
func (e *ConsensusEngine) Evaluate(votes []entity.Vote, round int, policy ConsensusPolicy) entity.ConsensusResult {
	if len(votes) == 0 {
		return entity.ConsensusResult{Outcome: entity.ConsensusInsufficientQuorum, Round: round, Detail: "no votes"}
	}

	approve, reject, abstain := 0, 0, 0
	for _, v := range votes {
		d := v.Decision
		if d == entity.VoteDecisionConditionalApprove {
			if policy.ConditionalAsApprove {
				d = entity.VoteDecisionApprove
			} else {
				d = entity.VoteDecisionAbstain
			}
		}
		switch d {
		case entity.VoteDecisionApprove:
			approve++
		case entity.VoteDecisionReject:
			reject++
		default:
			abstain++
		}
	}

	effective := len(votes) - abstain
	if effective < policy.Quorum {
		return entity.ConsensusResult{
			Outcome: entity.ConsensusInsufficientQuorum, Votes: votes, Round: round,
			Detail: fmt.Sprintf("effective %d < quorum %d", effective, policy.Quorum),
		}
	}

	// 3:0 strong consensus
	if approve == len(votes) {
		return entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Votes: votes, Round: round, Detail: "unanimous approval"}
	}
	if reject == len(votes) {
		return entity.ConsensusResult{Outcome: entity.ConsensusStrongRejection, Votes: votes, Round: round, Detail: "unanimous rejection"}
	}

	// 2:1 majority
	if approve >= 2 && approve > reject {
		detail := "majority approval"
		if round == 1 && policy.FirstSplitGoesToDebate {
			detail = "first round split, debate recommended"
		} else if round >= 2 && policy.ResolveOnReconsiderMajority {
			detail = "reconsider majority, resolve recommended"
		}
		return entity.ConsensusResult{Outcome: entity.ConsensusMajorityApprovalDissent, Votes: votes, Round: round, Detail: detail}
	}
	if reject >= 2 && reject > approve {
		detail := "majority rejection"
		if round == 1 && policy.FirstSplitGoesToDebate {
			detail = "first round split, debate recommended"
		} else if round >= 2 && policy.ResolveOnReconsiderMajority {
			detail = "reconsider majority, resolve recommended"
		}
		return entity.ConsensusResult{Outcome: entity.ConsensusMajorityRejectionDissent, Votes: votes, Round: round, Detail: detail}
	}

	// deadlock: no majority (e.g. approve=1, reject=1, abstain=1)
	return entity.ConsensusResult{Outcome: entity.ConsensusDeadlock, Votes: votes, Round: round, Detail: fmt.Sprintf("approve=%d reject=%d abstain=%d", approve, reject, abstain)}
}
