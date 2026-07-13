package consensus

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func vote(d entity.VoteDecision) entity.Vote { return entity.Vote{Decision: d} }

func TestEvaluate(t *testing.T) {
	def := DefaultConsensusPolicy()
	tests := []struct {
		name     string
		votes    []entity.Vote
		round    int
		policy   ConsensusPolicy
		want     entity.ConsensusOutcome
	}{
		{"3 approve -> strong", []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove)}, 1, def, entity.ConsensusStrongApproval},
		{"3 reject -> strong", []entity.Vote{vote(entity.VoteDecisionReject), vote(entity.VoteDecisionReject), vote(entity.VoteDecisionReject)}, 1, def, entity.ConsensusStrongRejection},
		{"2:1 approve round 1", []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 1, def, entity.ConsensusMajorityApprovalDissent},
		{"2:1 approve round 2", []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 2, def, entity.ConsensusMajorityApprovalDissent},
		{"2:1 reject", []entity.Vote{vote(entity.VoteDecisionReject), vote(entity.VoteDecisionReject), vote(entity.VoteDecisionApprove)}, 1, def, entity.ConsensusMajorityRejectionDissent},
		{"abstain quorum fail", []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionAbstain), vote(entity.VoteDecisionAbstain)}, 1, def, entity.ConsensusInsufficientQuorum},
		{"3 different -> deadlock", []entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject), vote(entity.VoteDecisionAbstain)}, 1, def, entity.ConsensusDeadlock},
		{"conditional as approve", []entity.Vote{vote(entity.VoteDecisionConditionalApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 1, def, entity.ConsensusMajorityApprovalDissent},
		{"conditional as abstain", []entity.Vote{vote(entity.VoteDecisionConditionalApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 1, ConsensusPolicy{Quorum: 3, ConditionalAsApprove: false}, entity.ConsensusInsufficientQuorum},
		{"empty votes", []entity.Vote{}, 1, def, entity.ConsensusInsufficientQuorum},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := NewConsensusEngine()
			got := eng.Evaluate(tt.votes, tt.round, tt.policy)
			if got.Outcome != tt.want {
				t.Fatalf("outcome=%s want=%s detail=%s", got.Outcome, tt.want, got.Detail)
			}
		})
	}
}

func TestEvaluate_FirstRoundDetail(t *testing.T) {
	eng := NewConsensusEngine()
	def := DefaultConsensusPolicy()
	res := eng.Evaluate([]entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 1, def)
	if res.Detail != "first round split, debate recommended" {
		t.Fatalf("detail: %s", res.Detail)
	}
}

func TestEvaluate_ReconsiderDetail(t *testing.T) {
	eng := NewConsensusEngine()
	def := DefaultConsensusPolicy()
	res := eng.Evaluate([]entity.Vote{vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionApprove), vote(entity.VoteDecisionReject)}, 2, def)
	if res.Detail != "reconsider majority, resolve recommended" {
		t.Fatalf("detail: %s", res.Detail)
	}
}
