package memory

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// ContextBuilder assembles AgentContext from a DecisionCase + DecisionTask +
// RAG-retrieved knowledge + optional debate/previous state (ADR-006).
type ContextBuilder struct {
	knowledge port.KnowledgePort
}

func NewContextBuilder(knowledge port.KnowledgePort) *ContextBuilder {
	return &ContextBuilder{knowledge: knowledge}
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
		if err == nil {
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

func caseID(c *entity.DecisionCase) string {
	if c == nil {
		return ""
	}
	return c.ID
}
