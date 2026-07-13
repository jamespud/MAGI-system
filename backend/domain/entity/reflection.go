package entity

import "time"

// Reflection is a Magi's reconsideration output after a debate round.
type Reflection struct {
	ID                           string                        `json:"id,omitempty"`
	AgentRunID                   string                        `json:"agent_run_id,omitempty"`
	Round                        int                           `json:"round,omitempty"`
	PreviousVoteID               string                        `json:"previous_vote_id,omitempty"`
	PositionChange               PositionChange                `json:"position_change"`
	AcceptedClaims               []string                      `json:"accepted_claims,omitempty"`
	RejectedClaims               []string                      `json:"rejected_claims,omitempty"`
	NewEvidenceIDs               []string                      `json:"new_evidence_ids,omitempty"`
	UtilityDimensionReevaluation *UtilityDimensionReevaluation `json:"utility_dimension_reevaluation,omitempty"`
	Reasoning                    string                        `json:"reasoning"`
	ReadyToRevote                bool                          `json:"ready_to_revote"`
	CreatedAt                    time.Time                     `json:"created_at,omitempty"`
}

// UtilityDimensionReevaluation captures which utility dimensions were
// re-evaluated (ADR-009, criterion 4).
type UtilityDimensionReevaluation struct {
	DimensionsReEvaluated []string               `json:"dimensions_re_evaluated"`
	ScoreChanges          []DimensionScoreChange `json:"score_changes"`
}

// DimensionScoreChange records a before/after score for a re-evaluated dimension.
type DimensionScoreChange struct {
	DimensionCode string  `json:"dimension_code"`
	PreviousScore float64 `json:"previous_score"`
	NewScore      float64 `json:"new_score"`
	Reason        string  `json:"reason"`
}

type PositionChange string

const (
	PositionChangeMaintain   PositionChange = "maintain"
	PositionChangeStrengthen PositionChange = "strengthen"
	PositionChangeWeaken     PositionChange = "weaken"
	PositionChangeChange     PositionChange = "change"
	PositionChangeAbstain    PositionChange = "abstain"
)
