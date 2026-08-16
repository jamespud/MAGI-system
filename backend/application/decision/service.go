package decision

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/jamespud/magi/backend/application/tracing"
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

// WithToolCallRepo injects a ToolCallRepository for the /agents endpoint.
func WithToolCallRepo(repo port.ToolCallRepository) Option {
	return func(s *Service) { s.toolCallRepo = repo }
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
	toolCallRepo port.ToolCallRepository
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

// Create creates a new DecisionCase owned by userID (0 = unowned/open mode).
func (s *Service) Create(ctx context.Context, userID int64, question, background string, constraints []entity.Constraint) (*entity.DecisionCase, error) {
	case_ := &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%s", uuid.NewString()),
		UserID:          userID,
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
	ctx, span := tracing.Start(ctx, "decision.run",
		attribute.String("case.id", case_.ID), attribute.Int64("user.id", case_.UserID))
	defer span.End()
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
	if err := s.runs.Start(ctx, case_); err != nil {
		return err
	}
	// Execution is asynchronous; surface an in-progress status so the caller
	// (and the sidebar) sees the case as running immediately.
	case_.Status = entity.CaseStatusNormalizing
	if s.caseRepo != nil {
		_ = s.caseRepo.UpdateStatus(ctx, case_.ID, case_.Status)
	}
	return nil
}

// ForkAndRun creates a new case inheriting the source case's decision context
// (question/background/constraints) and starts it immediately. The fork links
// back via ParentCaseID, while the run executes under the current tool/RAG
// environment — the fork's own context — so rerunning leverages capabilities
// the original case did not have.
func (s *Service) ForkAndRun(ctx context.Context, userID int64, sourceCaseID string) (*entity.DecisionCase, error) {
	if s.caseRepo == nil {
		return nil, fmt.Errorf("fork: case repository is required")
	}
	src, err := s.caseRepo.Get(ctx, sourceCaseID)
	if err != nil || src == nil {
		return nil, fmt.Errorf("fork: source case %q not found", sourceCaseID)
	}
	fork := &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%s", uuid.NewString()),
		UserID:          userID,
		Question:        src.Question,
		Context:         src.Context,
		Constraints:     src.Constraints,
		MaxDebateRounds: s.cfg.MaxDebateRounds,
		Status:          entity.CaseStatusDraft,
		ParentCaseID:    src.ID,
		CreatedAt:       time.Now(),
	}
	if err := s.caseRepo.Create(ctx, fork); err != nil {
		return nil, fmt.Errorf("fork: create: %w", err)
	}
	if err := s.StartRun(ctx, fork); err != nil {
		return nil, fmt.Errorf("fork: start run: %w", err)
	}
	return fork, nil
}

// CancelRun cancels an active run. Returns true if a run was active.
func (s *Service) CancelRun(caseID string) bool {
	if s.runs == nil {
		return false
	}
	return s.runs.Cancel(caseID)
}

// List returns all decision cases (requires CaseRepository). Legacy helper
// retained for in-memory/test repositories and admin aggregates; the
// user-facing list goes through ListPaged (P2 D10).
func (s *Service) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	if s.caseRepo != nil {
		return s.caseRepo.List(ctx)
	}
	return nil, nil
}

// ListScoped returns a user-scoped, paginated page of cases. It prefers the
// database-side multi-tenant query (port.CaseListFilter, P0: D2); repositories
// that do not implement it fall back to the legacy full List plus in-memory
// owner filtering and paging.
func (s *Service) ListScoped(ctx context.Context, userID int64, limit, offset int) ([]*entity.DecisionCase, error) {
	if s.caseRepo == nil {
		return nil, nil
	}
	if f, ok := s.caseRepo.(port.CaseListFilter); ok {
		return f.ListForUser(ctx, userID, limit, offset)
	}
	all, err := s.caseRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterCasesByUser(all, userID, limit, offset), nil
}

// filterCasesByUser applies owner scoping and paging in memory. It is only the
// fallback for repositories that do not implement port.CaseListFilter (test
// stubs, legacy in-memory repos); production repositories scope in SQL.
func filterCasesByUser(all []*entity.DecisionCase, userID int64, limit, offset int) []*entity.DecisionCase {
	var out []*entity.DecisionCase
	for _, cs := range all {
		if cs == nil {
			continue
		}
		if userID != 0 && cs.UserID != 0 && cs.UserID != userID {
			continue
		}
		out = append(out, cs)
	}
	if limit <= 0 {
		limit = len(out)
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(out) {
		return nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end]
}

// ListPaged returns one page of cases owned by userID (0 = open mode lists
// all), ordered by created_at DESC, plus the total count (P2 D10).
func (s *Service) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	if s.caseRepo == nil {
		return nil, 0, nil
	}
	return s.caseRepo.ListPaged(ctx, userID, page, pageSize)
}

// UpdateFlags updates pinned/archived list-management flags on a case (P2 D11).
func (s *Service) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	if s.caseRepo == nil {
		return fmt.Errorf("case repository not configured")
	}
	return s.caseRepo.UpdateFlags(ctx, id, pinned, archived)
}

// Delete removes a case and its artifacts (P2 D16).
func (s *Service) Delete(ctx context.Context, id string) error {
	if s.caseRepo == nil {
		return fmt.Errorf("case repository not configured")
	}
	return s.caseRepo.Delete(ctx, id)
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

// ToolCalls returns all tool-call records for a case.
func (s *Service) ToolCalls(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	if s.toolCallRepo == nil {
		return nil, nil
	}
	return s.toolCallRepo.ListByCase(ctx, caseID)
}

// Cancel cancels a DecisionCase by setting its status to CANCELLED.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if s.caseRepo != nil {
		return s.caseRepo.UpdateStatus(ctx, id, entity.CaseStatusCancelled)
	}
	return fmt.Errorf("case repository not configured")
}

// Report returns the final report for a resolved case, loading the resolution
// by caseID. Returns "" if no resolution is persisted yet.
func (s *Service) Report(ctx context.Context, caseID string) string {
	res, _ := s.Resolution(ctx, caseID)
	if res != nil {
		return res.FinalReport
	}
	return ""
}
