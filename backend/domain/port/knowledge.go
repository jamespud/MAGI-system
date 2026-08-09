package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// KnowledgeChunk is one retrieved knowledge fragment (used by AgentContext).
type KnowledgeChunk struct {
	Content   string
	Score     float64
	SourceURI string
}

// RetrieveRequest configures a knowledge retrieval.
type RetrieveRequest struct {
	Query      string
	TopK       int
	MinScore   float64
	Sources    []string
	SourceRefs []string
}

// MergedBlock is a retrieval result block at one of the three hierarchy levels.
type MergedBlock struct {
	Level     int
	Content   string
	SourceRef string
	ChunkIDs  []string
}

// RetrieveResult is the outcome of a retrieval.
type RetrieveResult struct {
	Blocks []MergedBlock
}

// StoreStats reports how many chunks were persisted for one case-memory
// projection. Used for observability (logs + MEMORY_INDEXED event payload).
type StoreStats struct {
	Chunks300  int
	Chunks900  int
	Chunks1800 int
}

// KnowledgePort wraps the RAG pipeline for retrieval + case-memory storage.
type KnowledgePort interface {
	Retrieve(ctx context.Context, req RetrieveRequest) (RetrieveResult, error)
	Store(ctx context.Context, proj *entity.CaseMemoryProjection) (StoreStats, error)
}
