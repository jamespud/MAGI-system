package decision

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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

// Option configures a Service.
type Option func(*Service)

// WithCaseRepo injects a CaseRepository for persistence.
func WithCaseRepo(repo port.CaseRepository) Option {
	return func(s *Service) { s.caseRepo = repo }
}

// Service is the application-layer service for decision cases.
type Service struct {
	orch     Orchestrator
	cfg      ServiceConfig
	caseRepo port.CaseRepository
}

// NewService creates a DecisionService.
func NewService(orch Orchestrator, cfg ServiceConfig, opts ...Option) *Service {
	s := &Service{orch: orch, cfg: cfg}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create creates a new DecisionCase from a question, optional background, and constraints.
func (s *Service) Create(ctx context.Context, question, background string, constraints []entity.Constraint) (*entity.DecisionCase, error) {
	case_ := &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%d", time.Now().Unix()),
		Question:        question,
		Context:         background,
		Constraints:     constraints,
		MaxDebateRounds: s.cfg.MaxDebateRounds,
		Status:          entity.CaseStatusDraft,
		CreatedAt:       time.Now(),
	}
	if s.caseRepo != nil {
		if err := s.caseRepo.Create(ctx, case_); err != nil {
			return nil, fmt.Errorf("failed to persist case: %w", err)
		}
	}
	return case_, nil
}

// Run executes the orchestrator on a DecisionCase.
func (s *Service) Run(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	return s.orch.Orchestrate(ctx, case_)
}

// List returns all decision cases (requires CaseRepository).
func (s *Service) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	if s.caseRepo != nil {
		return s.caseRepo.List(ctx)
	}
	return nil, nil
}

// Get retrieves a DecisionCase by ID.
func (s *Service) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	if s.caseRepo != nil {
		return s.caseRepo.Get(ctx, id)
	}
	return nil, fmt.Errorf("case repository not configured")
}

// Cancel cancels a DecisionCase by setting its status to CANCELLED.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if s.caseRepo != nil {
		return s.caseRepo.UpdateStatus(ctx, id, entity.CaseStatusCancelled)
	}
	return fmt.Errorf("case repository not configured")
}

// Report returns the final report for a resolved case.
func (s *Service) Report(ctx context.Context, case_ *entity.DecisionCase, resolution *entity.Resolution) string {
	if resolution != nil {
		return resolution.FinalReport
	}
	return ""
}
