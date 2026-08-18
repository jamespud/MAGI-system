package investigationplan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages the editable investigation plan for a case.
type Service struct {
	repo port.InvestigationPlanRepository
}

func NewService(repo port.InvestigationPlanRepository) *Service {
	return &Service{repo: repo}
}

// Get returns the plan for a case, or nil when absent.
func (s *Service) Get(ctx context.Context, caseID string) (*entity.InvestigationPlan, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("investigation plan: repository not configured")
	}
	return s.repo.Get(ctx, caseID)
}

// Save validates and persists the plan.
func (s *Service) Save(ctx context.Context, caseID string, items []entity.InvestigationPlanItem) (*entity.InvestigationPlan, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("investigation plan: repository not configured")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("investigation plan: at least one item is required")
	}
	for i, item := range items {
		if strings.TrimSpace(item.Question) == "" {
			return nil, fmt.Errorf("investigation plan: item %d question is required", i)
		}
	}
	plan := &entity.InvestigationPlan{CaseID: caseID, Items: items, UpdatedAt: time.Now()}
	if err := s.repo.Save(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}
