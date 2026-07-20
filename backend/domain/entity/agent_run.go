package entity

import "time"

// AgentRun is one Magi's run within a case (one round).
type AgentRun struct {
	ID           string
	CaseID       string
	MagiConfigID string
	MagiCode     MagiCode
	Round        int
	Status       AgentRunStatus
	StartedAt    time.Time
	CompletedAt  *time.Time
	Usage        *Usage
	Err          string
}

type AgentRunStatus string

const (
	AgentRunStatusRunning   AgentRunStatus = "running"
	AgentRunStatusCompleted AgentRunStatus = "completed"
	AgentRunStatusFailed    AgentRunStatus = "failed"
	AgentRunStatusCancelled AgentRunStatus = "cancelled"
	AgentRunStatusMaxSteps  AgentRunStatus = "max_steps"
	AgentRunStatusTimedOut  AgentRunStatus = "timed_out"
)

// AgentState is a working-memory snapshot for checkpoint/resume.
type AgentState struct {
	RunID        string
	Messages     []MessageRef
	MessagesJSON string // full []*schema.Message JSON for non-lossy resume
	StepCount    int
	TokenUsed    int
	Phase        string
}

type MessageRef struct {
	Role    string
	Content string
}

// Usage aggregates token usage.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}
