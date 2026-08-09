package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service is the application-layer service for human-in-the-loop approvals.
type Service struct {
	repo port.ApprovalRepository
}

func NewService(repo port.ApprovalRepository) *Service {
	return &Service{repo: repo}
}

// Create persists a new approval request.
func (s *Service) Create(ctx context.Context, a *entity.ApprovalRequest) (*entity.ApprovalRequest, error) {
	if a == nil {
		return nil, fmt.Errorf("approval: request is required")
	}
	if a.ID == "" {
		a.ID = "appr-" + uuid.NewString()
	}
	if a.Status == "" {
		a.Status = entity.ApprovalPending
	}
	if a.RequestedAt.IsZero() {
		a.RequestedAt = time.Now()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("approval: create: %w", err)
	}
	return a, nil
}

func (s *Service) Get(ctx context.Context, id string) (*entity.ApprovalRequest, error) {
	if id == "" {
		return nil, fmt.Errorf("approval: id is required")
	}
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("approval: get: %w", err)
	}
	return a, nil
}

// List returns approvals for one case, or all approvals when caseID is empty.
func (s *Service) List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error) {
	if caseID != "" {
		return s.repo.List(ctx, caseID)
	}
	return s.repo.ListAll(ctx)
}

func (s *Service) Approve(ctx context.Context, id, decidedBy, reason string) (*entity.ApprovalRequest, error) {
	if err := s.repo.Approve(ctx, id, decidedBy, reason); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Reject(ctx context.Context, id, decidedBy, reason string) (*entity.ApprovalRequest, error) {
	if err := s.repo.Reject(ctx, id, decidedBy, reason); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}
