package consensus

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

type ConsensusEngine struct{}

func NewConsensusEngine() *ConsensusEngine { return &ConsensusEngine{} }

// BallotSet is one round's authoritative input: the ballots that were actually
// cast, and the expected participants that produced none. A non-vote is never
// turned into a ballot.
type BallotSet struct {
	Votes    []entity.Vote
	Absences []entity.AgentAbsence
}

// Evaluate is the legacy entry point for a round with no recorded absences. It
// is kept for callers that only hold ballots; EvaluateBallots is the contract
// the orchestrator uses.
func (e *ConsensusEngine) Evaluate(votes []entity.Vote, round int, policy ConsensusPolicy) entity.ConsensusResult {
	return e.EvaluateBallots(BallotSet{Votes: votes}, round, policy)
}

// EvaluateBallots counts the authoritative ballots of one round and classifies
// the outcome deterministically (ADR-009). It does NOT decide state transitions
// (debate vs resolve); the orchestrator inspects the Outcome + Detail + policy
// to choose the next state.
//
// A round is INCOMPLETE when an expected participant produced no ballot or when
// a ballot carries an unknown decision value. Such a round cannot produce a
// decision however many of the remaining ballots approve, because a failed,
// timed-out, cancelled, missing or malformed vote is not an abstention.
func (e *ConsensusEngine) EvaluateBallots(ballots BallotSet, round int, policy ConsensusPolicy) entity.ConsensusResult {
	for _, v := range ballots.Votes {
		if !entity.IsValidVoteDecision(v.Decision) {
			return entity.ConsensusResult{
				Outcome: entity.ConsensusIncomplete, Votes: ballots.Votes, Round: round,
				Detail: fmt.Sprintf("invalid vote decision %q", v.Decision),
			}
		}
	}
	if len(ballots.Absences) > 0 {
		return entity.ConsensusResult{
			Outcome: entity.ConsensusIncomplete, Votes: ballots.Votes, Round: round,
			Detail: absencesDetail(ballots.Absences),
		}
	}
	votes := ballots.Votes
	if len(votes) == 0 {
		return entity.ConsensusResult{Outcome: entity.ConsensusInsufficientQuorum, Round: round, Detail: "no votes"}
	}

	approve, reject, abstain := 0, 0, 0
	condCount := 0
	var conditions []entity.DecisionCondition
	for _, v := range votes {
		d := v.Decision
		if d == entity.VoteDecisionConditionalApprove {
			condCount++
			conditions = append(conditions, v.Conditions...)
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
		case entity.VoteDecisionAbstain:
			abstain++
		default:
			// Unreachable: unknown values are rejected above. Kept explicit so a
			// future decision value cannot silently become an abstention again.
			return entity.ConsensusResult{
				Outcome: entity.ConsensusIncomplete, Votes: votes, Round: round,
				Detail: fmt.Sprintf("invalid vote decision %q", d),
			}
		}
	}

	effective := len(votes) - abstain
	if effective < policy.Quorum {
		return entity.ConsensusResult{
			Outcome: entity.ConsensusInsufficientQuorum, Votes: votes, Round: round,
			Detail: fmt.Sprintf("effective %d < quorum %d", effective, policy.Quorum),
		}
	}

	// ConsensusConditional: an approval majority that includes conditional votes
	// surfaces the conditions instead of a plain approval (design §15).
	if policy.ConditionalAsApprove && condCount > 0 && approve >= 2 && approve > reject {
		return entity.ConsensusResult{
			Outcome:    entity.ConsensusConditional,
			Votes:      votes,
			Round:      round,
			Detail:     fmt.Sprintf("conditional approval, %d condition(s)", len(conditions)),
			Conditions: conditions,
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

// absencesDetail renders the missing participants for logs and case failures.
func absencesDetail(absences []entity.AgentAbsence) string {
	parts := make([]string, 0, len(absences))
	for _, a := range absences {
		parts = append(parts, a.String())
	}
	return fmt.Sprintf("%d participant(s) produced no ballot: %s", len(absences), strings.Join(parts, "; "))
}
