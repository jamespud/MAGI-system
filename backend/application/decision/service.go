package decision

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Orchestrator is the domain orchestrator interface (satisfied by
// orchestration.Orchestrator implicitly).
type Orchestrator interface {
	Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error)
}

// ServiceConfig holds application-level config for DecisionService.
type ServiceConfig struct {
	MaxDebateRounds int
}

// Service is the application-layer service for decision cases.
type Service struct {
	orch Orchestrator
	cfg  ServiceConfig
}

// NewService creates a DecisionService.
func NewService(orch Orchestrator, cfg ServiceConfig) *Service {
	return &Service{orch: orch, cfg: cfg}
}

// Create creates a new DecisionCase from a question.
func (s *Service) Create(ctx context.Context, question string) (*entity.DecisionCase, error) {
	return &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%d", time.Now().Unix()),
		Question:        question,
		MaxDebateRounds: s.cfg.MaxDebateRounds,
		Status:          entity.CaseStatusDraft,
		CreatedAt:       time.Now(),
	}, nil
}

// Run executes the orchestrator on a DecisionCase.
func (s *Service) Run(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	return s.orch.Orchestrate(ctx, case_)
}
