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
	Query      string   // single-query convenience; used when Queries is empty
	Queries    []string // multi-query retrieval; when non-empty it takes precedence
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

// RAG source namespaces. Case memories and uploaded knowledge documents live
// in separate namespaces so retrieval can be scoped to one or the other.
const (
	SourceCaseMemory   = "case_memory"
	SourceKnowledgeDoc = "knowledge_doc"
)

// DocumentIndexer indexes and deletes arbitrary knowledge documents in the
// RAG pipeline. It is intentionally separate from KnowledgePort so existing
// case-memory fakes stay valid.
// MemoryIndexer updates and removes case-memory chunks in the RAG pipeline.
// It is separate from KnowledgePort so retrieval-only fakes remain valid.
type MemoryIndexer interface {
	Store(ctx context.Context, proj *entity.CaseMemoryProjection) (StoreStats, error)
	DeleteSource(ctx context.Context, source, sourceRef string) error
}

type DocumentIndexer interface {
	StoreDocument(ctx context.Context, doc *entity.KnowledgeDoc) (StoreStats, error)
	DeleteSource(ctx context.Context, source, sourceRef string) error
}

// KnowledgeRepository persists user-uploaded knowledge documents.
type KnowledgeRepository interface {
	Create(ctx context.Context, doc *entity.KnowledgeDoc) error
	Get(ctx context.Context, id string) (*entity.KnowledgeDoc, error)
	ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.KnowledgeDoc, error)
	Update(ctx context.Context, doc *entity.KnowledgeDoc) error
	Delete(ctx context.Context, id string) error
	// ListAll returns every knowledge document regardless of owner (admin
	// reindex path).
	ListAll(ctx context.Context) ([]*entity.KnowledgeDoc, error)
}
