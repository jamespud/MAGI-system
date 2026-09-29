package consensus

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Issue #10: an agent that produced no ballot is not an abstention. Two
// approvals plus one absence must not be counted as 2 approve + 1 abstain and
// therefore must not satisfy quorum.
func TestEvaluateBallots_AbsenceIsNotAnAbstention(t *testing.T) {
	eng := NewConsensusEngine()
	got := eng.EvaluateBallots(BallotSet{
		Votes: []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove)},
		Absences: []entity.AgentAbsence{{
			AgentCode: entity.MagiCodeMelchior, Kind: entity.AbsenceAgentFailed, Reason: "model timeout",
		}},
	}, 1, DefaultConsensusPolicy())

	if got.Outcome != entity.ConsensusIncomplete {
		t.Fatalf("outcome=%s want=%s detail=%s", got.Outcome, entity.ConsensusIncomplete, got.Detail)
	}
}

// An unknown decision value must be rejected, never silently counted as an
// abstention that lets two approvals look like a majority.
func TestEvaluateBallots_RejectsUnknownDecision(t *testing.T) {
	eng := NewConsensusEngine()
	got := eng.EvaluateBallots(BallotSet{
		Votes: []entity.Vote{
			vote(entity.VoteDecisionApprove),
			vote(entity.VoteDecisionApprove),
			vote(entity.VoteDecision("maybe")),
		},
	}, 1, DefaultConsensusPolicy())

	if got.Outcome != entity.ConsensusIncomplete {
		t.Fatalf("outcome=%s want=%s detail=%s", got.Outcome, entity.ConsensusIncomplete, got.Detail)
	}
}

// The legacy entry point must reject unknown values too: it used to count them
// through a default branch.
func TestEvaluate_RejectsUnknownDecision(t *testing.T) {
	eng := NewConsensusEngine()
	got := eng.Evaluate([]entity.Vote{
		vote(entity.VoteDecisionApprove),
		vote(entity.VoteDecisionApprove),
		vote(entity.VoteDecision("maybe")),
	}, 1, DefaultConsensusPolicy())

	if got.Outcome != entity.ConsensusIncomplete {
		t.Fatalf("outcome=%s want=%s detail=%s", got.Outcome, entity.ConsensusIncomplete, got.Detail)
	}
}

// A genuine abstention is a decision the persona made, so a complete ballot set
// keeps the existing arithmetic.
func TestEvaluateBallots_GenuineAbstentionStillCounts(t *testing.T) {
	eng := NewConsensusEngine()
	got := eng.EvaluateBallots(BallotSet{
		Votes: []entity.Vote{
			vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionAbstain),
		},
	}, 2, DefaultConsensusPolicy())

	if got.Outcome != entity.ConsensusMajorityApprovalDissent {
		t.Fatalf("outcome=%s want=%s detail=%s", got.Outcome, entity.ConsensusMajorityApprovalDissent, got.Detail)
	}
}
