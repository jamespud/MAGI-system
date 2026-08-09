package entity

import "time"

// ToolCall is one tool invocation by an agent during a run (persisted to
// magi_tool_call, S8). Mirrors runtime.ToolCallRecord plus case/run linkage.
type ToolCall struct {
	ID         string
	CaseID     string
	AgentRunID string
	ToolCallID string // the LLM-assigned tool-call id
	ToolName   string
	Arguments  string
	Valid      bool
	Result     string
	Err        string
	ApprovedBy string
	EvidenceID string // namespaced persisted EV-ID this call produced (may be empty)
	DurationMs int64
	CreatedAt  time.Time
}
