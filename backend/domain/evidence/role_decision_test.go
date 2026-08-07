package evidence_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

func TestValidateRoleDecision_BalthasarBlocksHighRiskApproval(t *testing.T) {
	policy := entity.DefaultRolePolicy("balthasar")
	risk := 0.8
	err := evidence.ValidateRoleDecision(
		&entity.Vote{Decision: entity.VoteDecisionApprove},
		&entity.EvidenceSummary{RoleAssessment: &entity.RoleAssessment{Risk: &entity.RiskAssessment{ResidualRisk: &risk}}},
		policy,
		entity.ObjectiveFunction{},
	)
	if err == nil {
		t.Fatal("expected Balthasar to block approval above residual-risk limit")
	}
}

func TestValidateRoleDecision_CasperRequiresOpportunityThreshold(t *testing.T) {
	policy := entity.DefaultRolePolicy("casper")
	score := 40.0
	err := evidence.ValidateRoleDecision(
		&entity.Vote{Decision: entity.VoteDecisionConditionalApprove},
		&entity.EvidenceSummary{RoleAssessment: &entity.RoleAssessment{Opportunity: &entity.OpportunityAssessment{OpportunityScore: &score}}},
		policy,
		entity.ObjectiveFunction{},
	)
	if err == nil {
		t.Fatal("expected Casper to block approval below opportunity threshold")
	}
}

func TestValidateRoleDecision_RejectionCanRepresentRoleDissent(t *testing.T) {
	policy := entity.DefaultRolePolicy("balthasar")
	risk := 0.95
	err := evidence.ValidateRoleDecision(
		&entity.Vote{Decision: entity.VoteDecisionReject},
		&entity.EvidenceSummary{RoleAssessment: &entity.RoleAssessment{Risk: &entity.RiskAssessment{ResidualRisk: &risk}}},
		policy,
		entity.ObjectiveFunction{},
	)
	if err != nil {
		t.Fatalf("rejection should remain valid as a conservative dissent: %v", err)
	}
}

func TestValidateRoleDecision_UsesWeightedObjectiveScore(t *testing.T) {
	policy := entity.DefaultRolePolicy("melchior")
	feasibility := 90.0
	err := evidence.ValidateRoleDecision(
		&entity.Vote{
			Decision: entity.VoteDecisionApprove,
			UtilityScores: []entity.UtilityDimensionScore{
				{DimensionCode: "correctness", Score: 40},
			},
		},
		&entity.EvidenceSummary{RoleAssessment: &entity.RoleAssessment{Technical: &entity.TechnicalAssessment{FeasibilityScore: &feasibility}}},
		policy,
		entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{{Code: "correctness", Weight: 1}}},
	)
	if err == nil {
		t.Fatal("expected weighted objective score to block a low-score approval")
	}
}
