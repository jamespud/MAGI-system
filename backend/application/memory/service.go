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
}

// NewService creates a MemoryService.
func NewService(knowledge port.KnowledgePort, memRepo port.MemoryRepository) *Service {
	return &Service{knowledge: knowledge, memRepo: memRepo}
}

// Get retrieves a case memory projection by case ID.
func (s *Service) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	if s.memRepo != nil {
		return s.memRepo.Get(ctx, caseID)
	}
	return nil, nil
}

// Store persists a case memory projection.
func (s *Service) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	if s.memRepo != nil {
		return s.memRepo.Save(ctx, proj)
	}
	return nil
}
