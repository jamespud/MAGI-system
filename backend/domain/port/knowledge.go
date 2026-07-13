package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// KnowledgeChunk is one retrieved knowledge fragment.
type KnowledgeChunk struct {
	Content   string
	Score     float64
	SourceURI string
}

// KnowledgePort wraps Coze crossknowledge for RAG retrieval + case-memory storage.
type KnowledgePort interface {
	Retrieve(ctx context.Context, query string, knowledgeIDs []int64) ([]KnowledgeChunk, error)
	Store(ctx context.Context, proj *entity.CaseMemoryProjection) error
}
