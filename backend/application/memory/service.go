package memory

import (
	"context"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service is the application-layer service for case memory.
type Service struct {
	knowledge port.KnowledgePort
	memRepo   port.MemoryRepository
	cases     port.CaseRepository
}

// Option configures a MemoryService.
type Option func(*Service)

// WithCaseRepo enables owner filtering on memory search.
func WithCaseRepo(repo port.CaseRepository) Option {
	return func(s *Service) { s.cases = repo }
}

// NewService creates a MemoryService.
func NewService(knowledge port.KnowledgePort, memRepo port.MemoryRepository, opts ...Option) *Service {
	s := &Service{knowledge: knowledge, memRepo: memRepo}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Get retrieves a case memory projection by case ID.
func (s *Service) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	if s.memRepo != nil {
		return s.memRepo.Get(ctx, caseID)
	}
	return nil, nil
}

// Search returns memory projections the user may access (owner-filtered),
// fusing two recall paths:
//
//  1. Semantic RAG retrieval (Milvus + ES) over indexed case-memory chunks,
//     in relevance order — this is what makes the Memory page genuinely
//     semantic instead of a bare SQL LIKE.
//  2. Deterministic SQL LIKE fallback, which catches resolved cases whose
//     projections have not yet been vector-indexed (indexing is async).
//
// Retrieval errors never fail the search: they degrade to the LIKE fallback
// so a degraded Milvus/ES cannot silently empty the Memory page.
func (s *Service) Search(ctx context.Context, userID int64, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	seen := make(map[string]bool)
	out := make([]*entity.CaseMemoryProjection, 0, limit)

	// 1) Semantic recall (best-effort).
	if s.knowledge != nil {
		res, err := s.knowledge.Retrieve(ctx, port.RetrieveRequest{
			Query:   query,
			TopK:    limit * 3,
			Sources: []string{port.SourceCaseMemory},
		})
		if err == nil {
			for _, blk := range res.Blocks {
				if len(out) >= limit {
					break
				}
				if blk.SourceRef == "" || seen[blk.SourceRef] {
					continue
				}
				proj, gerr := s.memRepo.Get(ctx, blk.SourceRef)
				if gerr != nil || proj == nil {
					continue
				}
				if !s.allowed(ctx, userID, proj.CaseID) {
					continue
				}
				seen[proj.CaseID] = true
				out = append(out, proj)
			}
		}
	}

	// 2) LIKE fallback fills the remainder (not-yet-indexed projections).
	if len(out) < limit && s.memRepo != nil {
		all, err := s.memRepo.Search(ctx, query, limit)
		if err != nil {
			// Only surface the error when semantic recall produced nothing.
			if len(out) == 0 {
				return nil, err
			}
		} else {
			for _, proj := range all {
				if len(out) >= limit {
					break
				}
				if proj == nil || seen[proj.CaseID] {
					continue
				}
				if !s.allowed(ctx, userID, proj.CaseID) {
					continue
				}
				seen[proj.CaseID] = true
				out = append(out, proj)
			}
		}
	}
	return out, nil
}

// allowed reports whether the user may see a projection. Open mode (userID 0)
// and unowned cases pass; when a case repository is configured and the case
// cannot be resolved the projection is treated as inaccessible.
func (s *Service) allowed(ctx context.Context, userID int64, caseID string) bool {
	if s.cases == nil {
		return true
	}
	case_, err := s.cases.Get(ctx, caseID)
	if err != nil || case_ == nil {
		return false
	}
	return userID == 0 || case_.UserID == 0 || case_.UserID == userID
}

// Store persists a case memory projection.
func (s *Service) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	if s.memRepo != nil {
		return s.memRepo.Save(ctx, proj)
	}
	return nil
}
