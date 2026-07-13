package service

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type CaseService struct {
	repo port.CaseRepository
}

func NewCaseService(repo port.CaseRepository) *CaseService {
	return &CaseService{repo: repo}
}

func (s *CaseService) Create(ctx context.Context, c *entity.DecisionCase) error {
	if s.repo == nil {
		return nil
	}
	return s.repo.Create(ctx, c)
}

func (s *CaseService) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.Get(ctx, id)
}

func (s *CaseService) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	if s.repo == nil {
		return nil
	}
	return s.repo.UpdateStatus(ctx, id, status)
}
