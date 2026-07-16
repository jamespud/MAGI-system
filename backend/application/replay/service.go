package replay

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	domainservice "github.com/jamespud/magi/backend/domain/service"
)

// Service is the application-layer service for replay/timeline/trace.
type Service struct {
	repo port.EventRepository
}

// NewService creates a ReplayService.
func NewService(repo port.EventRepository) *Service {
	return &Service{repo: repo}
}

// Replay returns all events for a case, sorted by timestamp (delegates to domain).
func (s *Service) Replay(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return domainservice.Replay(ctx, caseID, s.repo)
}

// Timeline returns events in chronological order (same as Replay in Phase 2;
// Phase 3 may add filtering/formatting).
func (s *Service) Timeline(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return s.Replay(ctx, caseID)
}
