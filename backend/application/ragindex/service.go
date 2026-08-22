package ragindex

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// reindexBatchSize throttles enqueue to avoid a MySQL write spike.
const reindexBatchSize = 100

// Service exposes admin operations for the RAG index queue.
type Service struct {
	repo        port.RagIndexJobRepository
	memRepo     port.MemoryRepository
	knowRepo    port.KnowledgeRepository
	maxAttempts int
}

func NewService(repo port.RagIndexJobRepository, memRepo port.MemoryRepository, knowRepo port.KnowledgeRepository, maxAttempts int) *Service {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &Service{repo: repo, memRepo: memRepo, knowRepo: knowRepo, maxAttempts: maxAttempts}
}

// Reindex enqueues an index job for every row of the selected source.
// source is "case_memory", "knowledge_doc", "all" or "" (all).
func (s *Service) Reindex(ctx context.Context, source string) (int, error) {
	switch source {
	case "", "all":
		n, err := s.reindexCaseMemory(ctx)
		if err != nil {
			return n, err
		}
		m, err := s.reindexKnowledgeDocs(ctx)
		return n + m, err
	case port.SourceCaseMemory:
		return s.reindexCaseMemory(ctx)
	case port.SourceKnowledgeDoc:
		return s.reindexKnowledgeDocs(ctx)
	default:
		return 0, fmt.Errorf("rag reindex: unknown source %q", source)
	}
}

func (s *Service) reindexCaseMemory(ctx context.Context) (int, error) {
	projs, err := s.memRepo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("rag reindex: list projections: %w", err)
	}
	n := 0
	for i, proj := range projs {
		if _, err := s.repo.Enqueue(ctx, entity.RagIndexJobKindIndex, port.SourceCaseMemory, proj.CaseID, s.maxAttempts); err != nil {
			return n, fmt.Errorf("rag reindex: enqueue %s: %w", proj.CaseID, err)
		}
		n++
		if i%reindexBatchSize == reindexBatchSize-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return n, nil
}

func (s *Service) reindexKnowledgeDocs(ctx context.Context) (int, error) {
	docs, err := s.knowRepo.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("rag reindex: list docs: %w", err)
	}
	n := 0
	for i, doc := range docs {
		if _, err := s.repo.Enqueue(ctx, entity.RagIndexJobKindIndex, port.SourceKnowledgeDoc, doc.ID, s.maxAttempts); err != nil {
			return n, fmt.Errorf("rag reindex: enqueue %s: %w", doc.ID, err)
		}
		n++
		if i%reindexBatchSize == reindexBatchSize-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return n, nil
}
