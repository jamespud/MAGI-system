package entity

// RolePolicy is the executable contract that makes a Magi role more than a
// prompt label. The runtime and evidence gate use it to require role-specific
// analysis and to reject decisions that violate the role's decision boundary.
type RolePolicy struct {
	Role                    string
	EnforceAssessment       bool
	RequiredAssessment      string
	MaxResidualRisk         float64
	MinTechnicalScore       float64
	MinOpportunityScore     float64
	MinWeightedUtilityScore float64
	DebateDirective         string
}

const (
	RoleAssessmentTechnical   = "technical"
	RoleAssessmentRisk        = "risk"
	RoleAssessmentOpportunity = "opportunity"
)

// DefaultRolePolicy returns the production contract for one of the three
// MAGI roles. Unknown roles remain compatible with the generic runtime.
func DefaultRolePolicy(code string) RolePolicy {
	switch code {
	case string(MagiCodeMelchior):
		return RolePolicy{
			Role:                    code,
			EnforceAssessment:       true,
			RequiredAssessment:      RoleAssessmentTechnical,
			MinTechnicalScore:       60,
			MinWeightedUtilityScore: 60,
			DebateDirective:         "Audit the disputed claims with quantitative and technical evidence; expose assumptions, benchmarks, and feasibility blockers.",
		}
	case string(MagiCodeBalthasar):
		return RolePolicy{
			Role:                    code,
			EnforceAssessment:       true,
			RequiredAssessment:      RoleAssessmentRisk,
			MaxResidualRisk:         0.35,
			MinWeightedUtilityScore: 60,
			DebateDirective:         "Stress-test the proposal's worst case, reversibility, rollback path, operational failure modes, and residual risk.",
		}
	case string(MagiCodeCasper):
		return RolePolicy{
			Role:                    code,
			EnforceAssessment:       true,
			RequiredAssessment:      RoleAssessmentOpportunity,
			MinOpportunityScore:     60,
			MinWeightedUtilityScore: 60,
			DebateDirective:         "Look for the strongest opportunity, timing advantage, user signal, and opportunity cost of waiting or choosing the alternative.",
		}
	default:
		return RolePolicy{Role: code}
	}
}

// RoleAssessment is the structured part of an EvidenceSummary that differs by
// Magi role. It is optional in the wire schema for backward compatibility, but
// production configs enable deterministic validation through RolePolicy.
type RoleAssessment struct {
	DimensionAssessments []DimensionAssessment  `json:"dimension_assessments,omitempty"`
	Technical            *TechnicalAssessment   `json:"technical,omitempty"`
	Risk                 *RiskAssessment        `json:"risk,omitempty"`
	Opportunity          *OpportunityAssessment `json:"opportunity,omitempty"`
}

type DimensionAssessment struct {
	DimensionCode string   `json:"dimension_code"`
	Score         *float64 `json:"score"`
	EvidenceIDs   []string `json:"evidence_ids"`
	Reasoning     string   `json:"reasoning"`
}

type TechnicalAssessment struct {
	FeasibilityScore        *float64 `json:"feasibility_score"`
	QuantitativeEvidenceIDs []string `json:"quantitative_evidence_ids"`
	Assumptions             []string `json:"assumptions"`
	Blockers                []string `json:"blockers"`
}

type RiskAssessment struct {
	WorstCase          string   `json:"worst_case"`
	ResidualRisk       *float64 `json:"residual_risk"`
	ReversibilityScore *float64 `json:"reversibility_score"`
	RollbackPlan       string   `json:"rollback_plan"`
	RiskEvidenceIDs    []string `json:"risk_evidence_ids"`
}

type OpportunityAssessment struct {
	OpportunityScore      *float64 `json:"opportunity_score"`
	TimeWindow            string   `json:"time_window"`
	OpportunityCost       string   `json:"opportunity_cost"`
	UserSignalEvidenceIDs []string `json:"user_signal_evidence_ids"`
}
