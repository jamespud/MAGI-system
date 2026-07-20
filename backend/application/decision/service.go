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

// WithEvidenceRepo injects an EvidenceRepository for the /evidence endpoint.
func WithEvidenceRepo(repo port.EvidenceRepository) Option {
	return func(s *Service) { s.evidenceRepo = repo }
}

// WithClaimRepo injects a ClaimRepository for the /claims endpoint.
func WithClaimRepo(repo port.ClaimRepository) Option {
	return func(s *Service) { s.claimRepo = repo }
}

// WithVoteRepo injects a VoteRepository for the /votes endpoint.
func WithVoteRepo(repo port.VoteRepository) Option {
	return func(s *Service) { s.voteRepo = repo }
}

// WithAgentRunRepo injects an AgentRunRepository for the /agents endpoint.
func WithAgentRunRepo(repo port.AgentRunRepository) Option {
	return func(s *Service) { s.agentRunRepo = repo }
}

// RunController is the async-run lifecycle interface satisfied by RunManager.
// Defined as an interface so tests can inject a fake without constructing a
// real RunManager + Orchestrator.
type RunController interface {
	Start(ctx context.Context, c *entity.DecisionCase) error
	Cancel(caseID string) bool
}

// WithRunManager injects an async run controller. When set, StartRun delegates
// to it (async); otherwise StartRun falls back to synchronous Run.
func WithRunManager(rm RunController) Option {
	return func(s *Service) { s.runs = rm }
}

// WithResolutionRepo injects a ResolutionRepository so Get can enrich the
// case response with consensus/round/confidence.
func WithResolutionRepo(repo port.ResolutionRepository) Option {
	return func(s *Service) { s.resRepo = repo }
}

// Service is the application-layer service for decision cases.
type Service struct {
	orch         Orchestrator
	cfg          ServiceConfig
	caseRepo     port.CaseRepository
	resRepo      port.ResolutionRepository
	evidenceRepo port.EvidenceRepository
	claimRepo    port.ClaimRepository
	voteRepo     port.VoteRepository
	agentRunRepo port.AgentRunRepository
	runs         RunController
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

// StartRun launches an async orchestration for the case. Returns
// ErrAlreadyRunning if the case already has an active run. When no RunManager
// is configured, falls back to synchronous Run.
func (s *Service) StartRun(ctx context.Context, case_ *entity.DecisionCase) error {
	if s.runs == nil {
		_, err := s.Run(ctx, case_)
		return err
	}
	return s.runs.Start(ctx, case_)
}

// CancelRun cancels an active run. Returns true if a run was active.
func (s *Service) CancelRun(caseID string) bool {
	if s.runs == nil {
		return false
	}
	return s.runs.Cancel(caseID)
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

// Resolution returns the persisted resolution for a case, or nil if none.
func (s *Service) Resolution(ctx context.Context, caseID string) (*entity.Resolution, error) {
	if s.resRepo == nil {
		return nil, nil
	}
	res, err := s.resRepo.Get(ctx, caseID)
	if err != nil {
		return nil, nil // not-found is non-fatal
	}
	return res, nil
}

// Evidence returns all evidence records for a case.
func (s *Service) Evidence(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error) {
	if s.evidenceRepo == nil {
		return nil, nil
	}
	return s.evidenceRepo.ListByCase(ctx, caseID)
}

// Claims returns all claims for a case.
func (s *Service) Claims(ctx context.Context, caseID string) ([]*entity.Claim, error) {
	if s.claimRepo == nil {
		return nil, nil
	}
	return s.claimRepo.ListByCase(ctx, caseID)
}

// Votes returns all votes for a case.
func (s *Service) Votes(ctx context.Context, caseID string) ([]*entity.Vote, error) {
	if s.voteRepo == nil {
		return nil, nil
	}
	return s.voteRepo.ListByCase(ctx, caseID)
}

// AgentRuns returns all agent runs for a case.
func (s *Service) AgentRuns(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	if s.agentRunRepo == nil {
		return nil, nil
	}
	return s.agentRunRepo.ListByCase(ctx, caseID)
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
