package evidence_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

func roleFloat(v float64) *float64 { return &v }

func roleObjective() entity.ObjectiveFunction {
	return entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{
		{Code: "primary", Weight: 0.6},
		{Code: "secondary", Weight: 0.4},
	}}
}

func roleDimensions() []entity.DimensionAssessment {
	return []entity.DimensionAssessment{
		{DimensionCode: "primary", Score: roleFloat(80), EvidenceIDs: []string{"EV-001"}, Reasoning: "supported"},
		{DimensionCode: "secondary", Score: roleFloat(70), EvidenceIDs: []string{"EV-001"}, Reasoning: "supported"},
	}
}

func roleLedger(t *testing.T, collector string) *evidence.EvidenceLedger {
	t.Helper()
	l := evidence.NewEvidenceLedger("case", "run", collector)
	l.Record("tc", "tool", "local", "", "observation", entity.ReliabilityScore{Final: 0.95})
	return l
}

func TestValidateRoleAssessment_RequiresDifferentRoleSections(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{"melchior", "melchior", "TECHNICAL_ASSESSMENT_MISSING"},
		{"balthasar", "balthasar", "RISK_ASSESSMENT_MISSING"},
		{"casper", "casper", "OPPORTUNITY_ASSESSMENT_MISSING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := entity.DefaultRolePolicy(tt.code)
			assessment := &entity.RoleAssessment{DimensionAssessments: roleDimensions()}
			violations := evidence.ValidateRoleAssessment(&entity.EvidenceSummary{RoleAssessment: assessment}, roleLedger(t, tt.code), policy, roleObjective(), tt.code, true)
			for _, v := range violations {
				if v.Code == tt.want {
					return
				}
			}
			t.Fatalf("expected %s, got %+v", tt.want, violations)
		})
	}
}

func TestValidateRoleAssessment_TechnicalPassesWithQuantitativeEvidence(t *testing.T) {
	policy := entity.DefaultRolePolicy("melchior")
	assessment := &entity.RoleAssessment{
		DimensionAssessments: roleDimensions(),
		Technical: &entity.TechnicalAssessment{
			FeasibilityScore:        roleFloat(85),
			QuantitativeEvidenceIDs: []string{"EV-001"},
			Assumptions:             []string{"benchmark represents production workload"},
		},
	}
	violations := evidence.ValidateRoleAssessment(&entity.EvidenceSummary{RoleAssessment: assessment}, roleLedger(t, "melchior"), policy, roleObjective(), "melchior", true)
	if len(violations) != 0 {
		t.Fatalf("expected technical role assessment to pass, got %+v", violations)
	}
}

func TestValidateRoleAssessment_NoToolsStillRequiresStructuredRoleReasoning(t *testing.T) {
	policy := entity.DefaultRolePolicy("balthasar")
	assessment := &entity.RoleAssessment{
		DimensionAssessments: roleDimensions(),
		Risk: &entity.RiskAssessment{
			WorstCase:          "data loss",
			ResidualRisk:       roleFloat(0.2),
			ReversibilityScore: roleFloat(80),
			RollbackPlan:       "restore the previous deployment",
		},
	}
	violations := evidence.ValidateRoleAssessment(&entity.EvidenceSummary{RoleAssessment: assessment}, evidence.NewEvidenceLedger("case", "run", "balthasar"), policy, roleObjective(), "balthasar", false)
	if len(violations) != 0 {
		t.Fatalf("no-tools role assessment should pass without EV-ID requirements, got %+v", violations)
	}
}
