package memory

import (
	"context"

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

// Search returns memory projections the user may access (owner-filtered).
func (s *Service) Search(ctx context.Context, userID int64, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	if s.memRepo == nil {
		return nil, nil
	}
	all, err := s.memRepo.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.CaseMemoryProjection, 0, len(all))
	for _, proj := range all {
		if proj == nil {
			continue
		}
		if s.cases != nil {
			case_, err := s.cases.Get(ctx, proj.CaseID)
			if err != nil || case_ == nil {
				continue
			}
			if userID != 0 && case_.UserID != 0 && case_.UserID != userID {
				continue
			}
		}
		out = append(out, proj)
	}
	return out, nil
}

// Store persists a case memory projection.
func (s *Service) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	if s.memRepo != nil {
		return s.memRepo.Save(ctx, proj)
	}
	return nil
}
