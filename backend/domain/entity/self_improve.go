package entity

import "time"

// SelfImproveSuggestion is one analyzed failure with a deterministic,
// human-reviewable improvement proposal. The harness never applies rule or
// prompt changes automatically: an operator reviews and applies each
// suggestion.
type SelfImproveSuggestion struct {
	ID            string
	CaseID        string
	RunID         string
	AgentCode     string
	Category      string // gate_failure | model_error | tool_error | validation | timeout | other
	Failure       string
	Summary       string
	SuggestedRule string
	PromptKey     string
	PromptContent string
	Status        string
	CreatedAt     time.Time
	AppliedAt     *time.Time
}

const (
	SelfImproveOpen      = "open"
	SelfImproveAnalyzing = "analyzing"
	SelfImproveApplied   = "applied"
	SelfImproveDismissed = "dismissed"
)

const (
	SelfImproveGateFailure = "gate_failure"
	SelfImproveModelError  = "model_error"
	SelfImproveToolError   = "tool_error"
	SelfImproveValidation  = "validation"
	SelfImproveTimeout     = "timeout"
	SelfImproveOther       = "other"
)
