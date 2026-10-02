package entity

import (
	"fmt"
	"time"
)

// EvidenceSummaryClaim is a claim asserted in an EvidenceSummary.
type EvidenceSummaryClaim struct {
	Statement   string   `json:"statement"`
	Supports    []string `json:"supports"`
	Contradicts []string `json:"contradicts"`
}

// EvidenceSummary is the structured output the Magi produces when it believes it
// has gathered enough evidence. EvidenceByType is the Magi's self-classification
// of its evidence (the gate verifies EV-ID reality, not semantic type).
type EvidenceSummary struct {
	EvidenceByType map[string][]string    `json:"evidence_by_type"`
	Claims         []EvidenceSummaryClaim `json:"claims"`
	RoleAssessment *RoleAssessment        `json:"role_assessment,omitempty"`
	Ready          bool                   `json:"ready"`
}

// ClaimSubmission is a structured output for incremental claim submission
// during the gather phase. The Magi can submit claims mid-investigation
// without waiting for EvidenceSummary.
type ClaimSubmission struct {
	Type   string                 `json:"type"` // always "claim_submission"
	Claims []EvidenceSummaryClaim `json:"claims"`
}

// Vote is a Magi's structured final decision for a round.
type Vote struct {
	ID                  string                  `json:"id,omitempty"`
	CaseID              string                  `json:"case_id,omitempty"`
	ExecutionGeneration int64                   `json:"execution_generation,omitempty"`
	AgentRunID          string                  `json:"agent_run_id,omitempty"`
	Round               int                     `json:"round,omitempty"`
	Decision            VoteDecision            `json:"decision"`
	Confidence          float64                 `json:"confidence"`
	UtilityScores       []UtilityDimensionScore `json:"utility_scores"`
	KeyClaimIDs         []string                `json:"key_claim_ids,omitempty"`
	EvidenceIDs         []string                `json:"evidence_ids"`
	ReasoningSummary    string                  `json:"reasoning_summary,omitempty"`
	Conditions          []DecisionCondition     `json:"conditions,omitempty"`
	CreatedAt           time.Time               `json:"created_at,omitempty"`
}

// NormalizeConfidence coerces a model-provided confidence value to a 0-100
// percentage. Models answer in both 0-1 and 0-100 scales; normalizing at
// every read keeps legacy rows and freshly parsed votes consistent.
func NormalizeConfidence(v float64) float64 {
	if v > 0 && v <= 1 {
		v *= 100
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

type VoteDecision string

const (
	VoteDecisionApprove            VoteDecision = "approve"
	VoteDecisionReject             VoteDecision = "reject"
	VoteDecisionAbstain            VoteDecision = "abstain"
	VoteDecisionConditionalApprove VoteDecision = "conditional_approve"
)

// IsValidVoteDecision reports whether a decision is one of the defined final
// decisions. Unknown values must be rejected explicitly: a malformed decision
// is not an abstention and must never be counted as one.
func IsValidVoteDecision(d VoteDecision) bool {
	switch d {
	case VoteDecisionApprove, VoteDecisionReject, VoteDecisionAbstain, VoteDecisionConditionalApprove:
		return true
	default:
		return false
	}
}

// AbsenceKind classifies a participant that produced no authoritative ballot.
// It exists because "the persona decided to abstain" and "no ballot arrived"
// are different facts and must stay distinguishable.
type AbsenceKind string

const (
	AbsenceAgentFailed AbsenceKind = "agent_failed"
	AbsenceTimeout     AbsenceKind = "timeout"
	AbsenceCancelled   AbsenceKind = "cancelled"
	AbsenceMissing     AbsenceKind = "missing"
	AbsenceInvalid     AbsenceKind = "invalid"
)

// AgentAbsence records one expected participant that produced no authoritative
// ballot, with the reason kept as an observable diagnostic. It is deliberately
// not a Vote: a non-vote must never enter the ballot set.
type AgentAbsence struct {
	AgentCode MagiCode
	Kind      AbsenceKind
	Reason    string
}

// String renders one absence for logs and error messages.
func (a AgentAbsence) String() string {
	return fmt.Sprintf("%s (%s): %s", a.AgentCode, a.Kind, a.Reason)
}

type UtilityDimensionScore struct {
	DimensionCode string   `json:"dimension_code"`
	Score         float64  `json:"score"`
	EvidenceIDs   []string `json:"evidence_ids"`
	ClaimIDs      []string `json:"claim_ids,omitempty"`
	Reasoning     string   `json:"reasoning"`
}

type DecisionCondition struct {
	Statement string `json:"statement"`
	MustHold  bool   `json:"must_hold"`
}
