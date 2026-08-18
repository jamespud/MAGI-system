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
	Environment  *RunEnvironment
}

// RunEnvironment snapshots the configuration a Magi run executed under, so
// forked runs stay comparable and traceable (which model/tools/knowledge
// index were actually in effect for this run).
type RunEnvironment struct {
	ModelName      string   `json:"model_name"`
	ModelBaseURL   string   `json:"model_base_url"`
	Tools          []string `json:"tools,omitempty"`
	KnowledgeIndex bool     `json:"knowledge_index"`
	ConfigVersion  int64    `json:"config_version"`
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

// ModelCostExtraKey carries provider-accurate estimated cost in an Eino
// message when the model port has selected a failover provider.
const ModelCostExtraKey = "magi_model_cost_usd"

// Usage aggregates token usage.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CostUSD          float64
}

// Cost returns the estimated cost for this usage at the given per-million
// token prices.
func (u *Usage) Cost(inPerMillion, outPerMillion float64) float64 {
	if u == nil {
		return 0
	}
	return float64(u.PromptTokens)/1e6*inPerMillion + float64(u.CompletionTokens)/1e6*outPerMillion
}
