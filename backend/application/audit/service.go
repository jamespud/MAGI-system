package audit

import (
	"context"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service records and lists audit events. A nil repository makes Record a
// no-op so the feature degrades gracefully when not configured.
type Service struct {
	repo port.AuditRepository
}

func NewService(repo port.AuditRepository) *Service {
	return &Service{repo: repo}
}

// Record persists one audit event, skipping empty/no-op entries.
func (s *Service) Record(ctx context.Context, e *entity.AuditEvent) error {
	if s.repo == nil || e == nil {
		return nil
	}
	if strings.TrimSpace(e.Action) == "" && strings.TrimSpace(e.Resource) == "" {
		return nil
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	return s.repo.Record(ctx, e)
}

// List returns audit events newest-first plus the total count.
func (s *Service) List(ctx context.Context, limit, offset int) ([]*entity.AuditEvent, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	return s.repo.List(ctx, limit, offset)
}
