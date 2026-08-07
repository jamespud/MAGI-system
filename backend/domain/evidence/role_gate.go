package evidence

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ValidateRoleAssessment enforces the role-specific part of an evidence
// summary. The generic evidence gate checks whether evidence exists; this gate
// checks whether the agent investigated the question through its own lens.
// hasTools controls whether real EV-ID references are mandatory. In no-tools
// mode the role still has to provide its structured reasoning, but fabricated
// evidence cannot be authenticated.
func ValidateRoleAssessment(
	summary *entity.EvidenceSummary,
	ledger *EvidenceLedger,
	policy entity.RolePolicy,
	objective entity.ObjectiveFunction,
	collector string,
	hasTools bool,
) []GateViolation {
	if !policy.EnforceAssessment {
		return nil
	}
	if summary == nil || summary.RoleAssessment == nil {
		return []GateViolation{{Code: "ROLE_ASSESSMENT_MISSING", Message: fmt.Sprintf("%s requires a structured %s assessment", policy.Role, policy.RequiredAssessment)}}
	}

	var out []GateViolation
	requireEvidence := hasTools && ledger != nil && len(ledger.List()) > 0
	out = append(out, validateDimensionAssessments(summary.RoleAssessment.DimensionAssessments, objective, ledger, collector, requireEvidence)...)

	switch policy.RequiredAssessment {
	case entity.RoleAssessmentTechnical:
		assessment := summary.RoleAssessment.Technical
		if assessment == nil {
			out = append(out, GateViolation{Code: "TECHNICAL_ASSESSMENT_MISSING", Message: "Melchior requires technical assessment"})
			break
		}
		if assessment.FeasibilityScore == nil || *assessment.FeasibilityScore < 0 || *assessment.FeasibilityScore > 100 {
			out = append(out, GateViolation{Code: "FEASIBILITY_SCORE_INVALID", Message: "technical feasibility_score must be in [0,100]"})
		}
		if len(assessment.Assumptions) == 0 {
			out = append(out, GateViolation{Code: "TECHNICAL_ASSUMPTIONS_MISSING", Message: "technical assessment requires at least one explicit assumption"})
		}
		if requireEvidence && len(assessment.QuantitativeEvidenceIDs) == 0 {
			out = append(out, GateViolation{Code: "QUANTITATIVE_EVIDENCE_MISSING", Message: "Melchior requires quantitative evidence IDs"})
		}
		out = append(out, validateEvidenceIDs(assessment.QuantitativeEvidenceIDs, ledger, collector, "QUANTITATIVE_EVIDENCE_INVALID")...)

	case entity.RoleAssessmentRisk:
		assessment := summary.RoleAssessment.Risk
		if assessment == nil {
			out = append(out, GateViolation{Code: "RISK_ASSESSMENT_MISSING", Message: "Balthasar requires risk assessment"})
			break
		}
		if strings.TrimSpace(assessment.WorstCase) == "" {
			out = append(out, GateViolation{Code: "WORST_CASE_DETAIL_MISSING", Message: "risk assessment requires a concrete worst_case"})
		}
		if strings.TrimSpace(assessment.RollbackPlan) == "" {
			out = append(out, GateViolation{Code: "ROLLBACK_PLAN_MISSING", Message: "risk assessment requires a rollback_plan"})
		}
		if assessment.ResidualRisk == nil || *assessment.ResidualRisk < 0 || *assessment.ResidualRisk > 1 {
			out = append(out, GateViolation{Code: "RESIDUAL_RISK_INVALID", Message: "residual_risk must be in [0,1]"})
		}
		if assessment.ReversibilityScore == nil || *assessment.ReversibilityScore < 0 || *assessment.ReversibilityScore > 100 {
			out = append(out, GateViolation{Code: "REVERSIBILITY_SCORE_INVALID", Message: "reversibility_score must be in [0,100]"})
		}
		if requireEvidence && len(assessment.RiskEvidenceIDs) == 0 {
			out = append(out, GateViolation{Code: "RISK_EVIDENCE_MISSING", Message: "Balthasar requires risk evidence IDs"})
		}
		out = append(out, validateEvidenceIDs(assessment.RiskEvidenceIDs, ledger, collector, "RISK_EVIDENCE_INVALID")...)

	case entity.RoleAssessmentOpportunity:
		assessment := summary.RoleAssessment.Opportunity
		if assessment == nil {
			out = append(out, GateViolation{Code: "OPPORTUNITY_ASSESSMENT_MISSING", Message: "Casper requires opportunity assessment"})
			break
		}
		if assessment.OpportunityScore == nil || *assessment.OpportunityScore < 0 || *assessment.OpportunityScore > 100 {
			out = append(out, GateViolation{Code: "OPPORTUNITY_SCORE_INVALID", Message: "opportunity_score must be in [0,100]"})
		}
		if strings.TrimSpace(assessment.TimeWindow) == "" {
			out = append(out, GateViolation{Code: "TIME_WINDOW_DETAIL_MISSING", Message: "opportunity assessment requires a concrete time_window"})
		}
		if strings.TrimSpace(assessment.OpportunityCost) == "" {
			out = append(out, GateViolation{Code: "OPPORTUNITY_COST_DETAIL_MISSING", Message: "opportunity assessment requires opportunity_cost"})
		}
		if requireEvidence && len(assessment.UserSignalEvidenceIDs) == 0 {
			out = append(out, GateViolation{Code: "USER_SIGNAL_EVIDENCE_MISSING", Message: "Casper requires user/trend evidence IDs"})
		}
		out = append(out, validateEvidenceIDs(assessment.UserSignalEvidenceIDs, ledger, collector, "USER_SIGNAL_EVIDENCE_INVALID")...)
	default:
		out = append(out, GateViolation{Code: "UNKNOWN_ROLE_ASSESSMENT", Message: fmt.Sprintf("unknown required assessment %q", policy.RequiredAssessment)})
	}
	return out
}

func validateDimensionAssessments(
	assessments []entity.DimensionAssessment,
	objective entity.ObjectiveFunction,
	ledger *EvidenceLedger,
	collector string,
	requireEvidence bool,
) []GateViolation {
	var out []GateViolation
	byCode := make(map[string]entity.DimensionAssessment, len(assessments))
	for _, a := range assessments {
		if _, exists := byCode[a.DimensionCode]; exists {
			out = append(out, GateViolation{Code: "DUPLICATE_DIMENSION_ASSESSMENT", Message: fmt.Sprintf("duplicate dimension assessment %q", a.DimensionCode)})
			continue
		}
		byCode[a.DimensionCode] = a
	}
	for _, d := range objective.Dimensions {
		a, ok := byCode[d.Code]
		if !ok {
			out = append(out, GateViolation{Code: "DIMENSION_ASSESSMENT_MISSING", Message: fmt.Sprintf("missing role assessment for dimension %q", d.Code)})
			continue
		}
		if a.Score == nil || *a.Score < 0 || *a.Score > 100 {
			out = append(out, GateViolation{Code: "DIMENSION_SCORE_INVALID", Message: fmt.Sprintf("dimension %q score must be in [0,100]", d.Code)})
		}
		if strings.TrimSpace(a.Reasoning) == "" {
			out = append(out, GateViolation{Code: "DIMENSION_REASONING_MISSING", Message: fmt.Sprintf("dimension %q requires reasoning", d.Code)})
		}
		if requireEvidence && len(a.EvidenceIDs) == 0 {
			out = append(out, GateViolation{Code: "DIMENSION_EVIDENCE_MISSING", Message: fmt.Sprintf("dimension %q requires evidence IDs", d.Code)})
		}
		out = append(out, validateEvidenceIDs(a.EvidenceIDs, ledger, collector, "DIMENSION_EVIDENCE_INVALID")...)
	}
	for _, a := range assessments {
		found := false
		for _, d := range objective.Dimensions {
			if d.Code == a.DimensionCode {
				found = true
				break
			}
		}
		if !found {
			out = append(out, GateViolation{Code: "UNKNOWN_DIMENSION_ASSESSMENT", Message: fmt.Sprintf("dimension %q is not in the role objective", a.DimensionCode)})
		}
	}
	return out
}

func validateEvidenceIDs(ids []string, ledger *EvidenceLedger, collector, code string) []GateViolation {
	if ledger == nil || len(ledger.List()) == 0 {
		return nil
	}
	var out []GateViolation
	for _, id := range ids {
		if !ledger.ExistsCollected(id, collector) {
			out = append(out, GateViolation{Code: code, Message: fmt.Sprintf("evidence ID %q is missing or not collected by %s", id, collector), Field: id})
		}
	}
	return out
}
