package entity

import "time"

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
	Ready          bool                   `json:"ready"`
}

// Vote is a Magi's structured final decision for a round.
type Vote struct {
	ID               string                 `json:"id,omitempty"`
	CaseID           string                 `json:"case_id,omitempty"`
	AgentRunID       string                 `json:"agent_run_id,omitempty"`
	Round            int                    `json:"round,omitempty"`
	Decision         VoteDecision           `json:"decision"`
	Confidence       float64                `json:"confidence"`
	UtilityScores    []UtilityDimensionScore `json:"utility_scores"`
	KeyClaimIDs      []string               `json:"key_claim_ids,omitempty"`
	EvidenceIDs      []string               `json:"evidence_ids"`
	ReasoningSummary string                 `json:"reasoning_summary,omitempty"`
	Conditions       []DecisionCondition    `json:"conditions,omitempty"`
	CreatedAt        time.Time              `json:"created_at,omitempty"`
}

type VoteDecision string

const (
	VoteDecisionApprove            VoteDecision = "approve"
	VoteDecisionReject             VoteDecision = "reject"
	VoteDecisionAbstain            VoteDecision = "abstain"
	VoteDecisionConditionalApprove VoteDecision = "conditional_approve"
)

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
