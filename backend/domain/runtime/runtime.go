package runtime

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

type RunInput struct {
	Config   *entity.MagiConfig
	Question string
	History  []*schema.Message
	MaxSteps int
	Timeout  time.Duration
}

type LoopStatus string

const (
	LoopStatusCompleted        LoopStatus = "completed"
	LoopStatusMaxSteps         LoopStatus = "max_steps"
	LoopStatusCancelled        LoopStatus = "cancelled"
	LoopStatusError            LoopStatus = "error"
	LoopStatusGateFailed       LoopStatus = "gate_failed"
	LoopStatusTokenBudget      LoopStatus = "token_budget_exceeded"
	LoopStatusToolFailures     LoopStatus = "tool_failures"
	LoopStatusValidationFailed LoopStatus = "validation_failed"
)

type LoopResult struct {
	FinalAnswer string
	Vote        *entity.Vote
	Summary     *entity.EvidenceSummary
	Reflection  *entity.Reflection
	Trace       *LoopTrace
	Ledger      *evidence.EvidenceLedger
	Usage       *entity.Usage
	Status      LoopStatus
	Err         error
}

type MagiRuntime interface {
	Run(ctx context.Context, cfg *entity.MagiConfig, actx *AgentContext) (*LoopResult, error)
}

type AgentContext struct {
	CaseID        string
	UserID        string
	RunID         string
	Task          entity.DecisionTask
	Constraints   []entity.Constraint
	ToolBindings  []entity.ToolBinding
	KnowledgeCtx  []port.KnowledgeChunk
	DebateContext *DebateContext
	PreviousRun   *PreviousAgentState
	// Execution carries the job fencing identity that a worker owns for this
	// attempt. It lets the runtime distinguish lease-owned work and refuse to
	// commit late results once the job lease is lost.
	Execution *ExecutionContext
}

// ExecutionContext identifies a worker-owned job attempt. JobID and RunID are
// the durable identities; WorkerID and JobAttempt are the physical worker/lease
// fencing fields used to reject late commits after a lease loss.
type ExecutionContext struct {
	JobID      string
	RunID      string
	WorkerID   string
	JobAttempt int
}

type DebateContext struct {
	Packet       entity.DebatePacket
	PreviousVote *entity.Vote
}

type PreviousAgentState struct {
	Summary *entity.EvidenceSummary
	Vote    *entity.Vote
}
