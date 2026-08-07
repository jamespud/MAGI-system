package evidence

import (
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ValidateRoleDecision prevents a role from emitting an approval that is
// inconsistent with its own structured assessment. This is deliberately
// deterministic: the LLM supplies scores, but the role boundary is enforced
// by code.
func ValidateRoleDecision(vote *entity.Vote, summary *entity.EvidenceSummary, policy entity.RolePolicy, objective entity.ObjectiveFunction) error {
	if !policy.EnforceAssessment || vote == nil {
		return nil
	}
	if vote.Decision != entity.VoteDecisionApprove && vote.Decision != entity.VoteDecisionConditionalApprove {
		return nil
	}
	if summary == nil || summary.RoleAssessment == nil {
		return fmt.Errorf("%s cannot approve without role assessment", policy.Role)
	}
	if policy.MinWeightedUtilityScore > 0 {
		weighted, err := weightedUtilityScore(vote, objective)
		if err != nil {
			return err
		}
		if weighted < policy.MinWeightedUtilityScore {
			return fmt.Errorf("weighted utility score %.1f is below %s approval threshold %.1f", weighted, policy.Role, policy.MinWeightedUtilityScore)
		}
	}

	switch policy.RequiredAssessment {
	case entity.RoleAssessmentTechnical:
		if summary.RoleAssessment.Technical == nil || summary.RoleAssessment.Technical.FeasibilityScore == nil {
			return fmt.Errorf("%s cannot approve without feasibility score", policy.Role)
		}
		if *summary.RoleAssessment.Technical.FeasibilityScore < policy.MinTechnicalScore {
			return fmt.Errorf("technical feasibility %.1f is below approval threshold %.1f", *summary.RoleAssessment.Technical.FeasibilityScore, policy.MinTechnicalScore)
		}
	case entity.RoleAssessmentRisk:
		if summary.RoleAssessment.Risk == nil || summary.RoleAssessment.Risk.ResidualRisk == nil {
			return fmt.Errorf("%s cannot approve without residual risk", policy.Role)
		}
		if *summary.RoleAssessment.Risk.ResidualRisk > policy.MaxResidualRisk {
			return fmt.Errorf("residual risk %.2f exceeds %s approval limit %.2f", *summary.RoleAssessment.Risk.ResidualRisk, policy.Role, policy.MaxResidualRisk)
		}
	case entity.RoleAssessmentOpportunity:
		if summary.RoleAssessment.Opportunity == nil || summary.RoleAssessment.Opportunity.OpportunityScore == nil {
			return fmt.Errorf("%s cannot approve without opportunity score", policy.Role)
		}
		if *summary.RoleAssessment.Opportunity.OpportunityScore < policy.MinOpportunityScore {
			return fmt.Errorf("opportunity score %.1f is below approval threshold %.1f", *summary.RoleAssessment.Opportunity.OpportunityScore, policy.MinOpportunityScore)
		}
	}
	return nil
}

func weightedUtilityScore(vote *entity.Vote, objective entity.ObjectiveFunction) (float64, error) {
	scores := make(map[string]float64, len(vote.UtilityScores))
	for _, score := range vote.UtilityScores {
		scores[score.DimensionCode] = score.Score
	}
	var total, weight float64
	for _, dimension := range objective.Dimensions {
		score, ok := scores[dimension.Code]
		if !ok {
			return 0, fmt.Errorf("missing utility score for dimension %q", dimension.Code)
		}
		total += score * dimension.Weight
		weight += dimension.Weight
	}
	if weight <= 0 {
		return 0, fmt.Errorf("role objective has no positive dimension weight")
	}
	return total / weight, nil
}
