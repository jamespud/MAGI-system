package runtime

import (
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

type LoopTrace struct {
	StartedAt   time.Time
	CompletedAt time.Time
	Status      LoopStatus
	Steps       []*Step
}

type Step struct {
	Index       int
	ModelOutput *schema.Message
	ToolCalls   []ToolCallRecord
	IsFinal     bool
	StartedAt   time.Time
	Duration    time.Duration
	ModelUsage  *entity.Usage
}

type ToolCallRecord struct {
	ToolCallID string
	ToolName   string
	Arguments  string
	EvidenceID string
	Valid      bool
	Violations []validation.Violation
	Result     string
	Err        string
	ApprovedBy string
	Duration   time.Duration
}

func extractUsage(msg *schema.Message) *entity.Usage {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return &entity.Usage{}
	}
	u := msg.ResponseMeta.Usage
	out := &entity.Usage{PromptTokens: int64(u.PromptTokens), CompletionTokens: int64(u.CompletionTokens), TotalTokens: int64(u.TotalTokens)}
	if cost, ok := msg.Extra[entity.ModelCostExtraKey].(float64); ok {
		out.CostUSD = cost
	}
	return out
}

func addUsage(a, b *entity.Usage) *entity.Usage {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &entity.Usage{PromptTokens: a.PromptTokens + b.PromptTokens, CompletionTokens: a.CompletionTokens + b.CompletionTokens, TotalTokens: a.TotalTokens + b.TotalTokens, CostUSD: a.CostUSD + b.CostUSD}
}

func finalizeTrace(trace *LoopTrace, status LoopStatus) {
	trace.CompletedAt = time.Now()
	trace.Status = status
}
