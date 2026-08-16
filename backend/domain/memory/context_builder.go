package memory

import (
	"context"
	"log"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// ContextBuilder assembles AgentContext from a DecisionCase + DecisionTask +
// RAG-retrieved knowledge + optional debate/previous state (ADR-006).
type ContextBuilder struct {
	knowledge port.KnowledgePort
	eventPub  port.EventPublisher
	metrics   *metrics.Registry
	logger    *log.Logger
}

// ContextBuilderOption configures a ContextBuilder.
type ContextBuilderOption func(*ContextBuilder)

// WithEventPublisher wires the event publisher so retrieval failures emit a
// MEMORY_RETRIEVAL_FAILED event instead of silently degrading (P0: D3).
func WithEventPublisher(p port.EventPublisher) ContextBuilderOption {
	return func(b *ContextBuilder) { b.eventPub = p }
}

// WithMetrics wires the metrics registry so retrieval failures are counted
// (P0: D3).
func WithMetrics(r *metrics.Registry) ContextBuilderOption {
	return func(b *ContextBuilder) { b.metrics = r }
}

func NewContextBuilder(knowledge port.KnowledgePort, opts ...ContextBuilderOption) *ContextBuilder {
	b := &ContextBuilder{knowledge: knowledge, logger: log.Default()}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *ContextBuilder) Build(
	ctx context.Context,
	case_ *entity.DecisionCase,
	task *entity.DecisionTask,
	debateContext *runtime.DebateContext,
	previousRun *runtime.PreviousAgentState,
) (*runtime.AgentContext, error) {
	query := ""
	if task != nil {
		query = task.CanonicalQuestion
	}
	if query == "" && case_ != nil {
		query = case_.Question
	}

	var chunks []port.KnowledgeChunk
	if b.knowledge != nil && query != "" {
		result, err := b.knowledge.Retrieve(ctx, port.RetrieveRequest{Query: query, TopK: 15})
		if err != nil {
			// P0: D3 — surface retrieval failures instead of silently degrading
			// the agent context. Log + metric + event make a RAG outage visible.
			b.recordRetrievalFailure(ctx, caseID(case_), err)
		} else {
			for _, blk := range result.Blocks {
				chunks = append(chunks, port.KnowledgeChunk{
					Content:   blk.Content,
					SourceURI: blk.SourceRef,
				})
			}
		}
	}

	var t entity.DecisionTask
	if task != nil {
		t = *task
	}
	var constraints []entity.Constraint
	if case_ != nil {
		constraints = case_.Constraints
	}

	return &runtime.AgentContext{
		CaseID:        caseID(case_),
		Task:          t,
		Constraints:   constraints,
		KnowledgeCtx:  chunks,
		DebateContext: debateContext,
		PreviousRun:   previousRun,
	}, nil
}

// recordRetrievalFailure logs the error, counts it on the metrics registry and
// publishes a MEMORY_RETRIEVAL_FAILED event so the failure is observable.
func (b *ContextBuilder) recordRetrievalFailure(ctx context.Context, caseID string, err error) {
	if b.logger != nil {
		b.logger.Printf("memory retrieve FAILED case=%s: %v", caseID, err)
	}
	if b.metrics != nil {
		b.metrics.IncMemoryRetrievalFailure()
	}
	if b.eventPub != nil {
		_ = b.eventPub.Publish(ctx, entity.NewEvent(caseID, "", nil, entity.EventMemoryRetrievalFailed, map[string]any{"error": err.Error()}))
	}
}

func caseID(c *entity.DecisionCase) string {
	if c == nil {
		return ""
	}
	return c.ID
}
