package port

import (
	"context"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Repository is the aggregate persistence port (S6 DB-backed impl; S1-S5 in-memory/test stubs).
type Repository interface {
	CaseRepo() CaseRepository
	AgentRunRepo() AgentRunRepository
	EvidenceRepo() EvidenceRepository
	ClaimRepo() ClaimRepository
	VoteRepo() VoteRepository
	DebateRepo() DebateRepository
	ReflectionRepo() ReflectionRepository
	ResolutionRepo() ResolutionRepository
	EventRepo() EventRepository
	CheckpointRepo() CheckpointRepository
	MemoryRepo() MemoryRepository
	ToolCallRepo() ToolCallRepository
	// PromptRepo exposes the versioned prompt registry (P2 D12).
	PromptRepo() PromptRepository
}

// DecisionJobRepository persists and leases asynchronous case execution.
// It is separate from Repository so existing aggregate fakes remain valid.
type DecisionJobRepository interface {
	Enqueue(ctx context.Context, caseID string, maxAttempts int) (*entity.DecisionJob, error)
	Claim(ctx context.Context, jobID, workerID string, leaseUntil time.Time) (*entity.DecisionJob, bool, error)
	Heartbeat(ctx context.Context, jobID, workerID string, leaseUntil time.Time) error
	MarkSucceeded(ctx context.Context, jobID, workerID string) error
	MarkFailed(ctx context.Context, jobID, workerID, lastError string, retryAt *time.Time) error
	Cancel(ctx context.Context, jobID string) error
	// MarkPaused parks a durable job so it is neither runnable nor counted as
	// active; a later ResumeQueued returns it to the runnable set.
	MarkPaused(ctx context.Context, jobID string) error
	ResumeQueued(ctx context.Context, jobID string) error
	RequeueExpired(ctx context.Context, now time.Time) error
	ListRunnable(ctx context.Context, now time.Time) ([]*entity.DecisionJob, error)
	GetByCase(ctx context.Context, caseID string) (*entity.DecisionJob, error)
	CountActiveByUser(ctx context.Context, userID int64) (int, error)
}
type CaseRepository interface {
	Create(ctx context.Context, c *entity.DecisionCase) error
	Get(ctx context.Context, id string) (*entity.DecisionCase, error)
	List(ctx context.Context) ([]*entity.DecisionCase, error)
	// ListPaged returns one page of cases ordered by created_at DESC, scoped to
	// userID when nonzero, plus the total number of matching cases. It replaces
	// the all-rows-then-filter pattern (P2 D10).
	ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error)
	UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error
	UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error
	// UpdateFlags updates list-management flags (pinned/archived). A nil
	// pointer leaves that flag unchanged.
	UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error
	// Delete removes a case and its artifacts (P2 D16).
	Delete(ctx context.Context, id string) error
}

// PauseStatusWriter is an optional CaseRepository capability that persists
// the pre-pause status together with the PAUSED status in one update, so a
// later wake can restore the FSM position. Repositories that do not implement
// it degrade to a plain status update (wake restarts from DRAFT).
type PauseStatusWriter interface {
	UpdatePaused(ctx context.Context, id string, status, pausedFrom entity.CaseStatus) error
}

// CaseListFilter is an optional capability for multi-tenant, paginated case
// listing. Repositories implementing it scope the SQL query to the owner and
// page it in the database instead of loading the whole table and filtering in
// memory (P0: D2). userID == 0 is open mode and returns all cases; a non-zero
// userID returns the user's own cases plus unowned (owner 0) cases, mirroring
// auth.CanAccess semantics.
type CaseListFilter interface {
	ListForUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.DecisionCase, error)
}

type AgentRunRepository interface {
	Create(ctx context.Context, r *entity.AgentRun) error
	Get(ctx context.Context, id string) (*entity.AgentRun, error)
	ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error)
	// SumUsageByUser returns the cumulative token usage and estimated cost for
	// all of a user's agent runs, used for per-user budget enforcement.
	SumUsageByUser(ctx context.Context, userID int64) (tokens int64, costUSD float64, err error)
	// CountByUser returns the number of agent runs owned by a user (P2 D9).
	CountByUser(ctx context.Context, userID int64) (int64, error)
}

type EvidenceRepository interface {
	Create(ctx context.Context, e *entity.EvidenceRecord) error
	Get(ctx context.Context, id string) (*entity.EvidenceRecord, error)
	ListByCase(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error)
}

type ClaimRepository interface {
	Create(ctx context.Context, c *entity.Claim) error
	Get(ctx context.Context, id string) (*entity.Claim, error)
	ListByCase(ctx context.Context, caseID string) ([]*entity.Claim, error)
}

type VoteRepository interface {
	Create(ctx context.Context, v *entity.Vote) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.Vote, error)
}

type DebateRepository interface {
	Create(ctx context.Context, d *entity.DebateRound) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.DebateRound, error)
}

type ReflectionRepository interface {
	Create(ctx context.Context, r *entity.Reflection) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.Reflection, error)
}

type ResolutionRepository interface {
	Create(ctx context.Context, r *entity.Resolution) error
	Get(ctx context.Context, caseID string) (*entity.Resolution, error)
}

type EventRepository interface {
	Create(ctx context.Context, e *entity.MagiEvent) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error)
	// ListAfter returns events for a case with Timestamp >= after, ordered by
	// Timestamp ascending. Used by SSE subscribers to poll for events that
	// other worker instances persisted (cross-instance live streaming).
	ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error)
}

type CheckpointRepository interface {
	Save(ctx context.Context, s *entity.AgentState) error
	Load(ctx context.Context, runID string) (*entity.AgentState, error)
}

type MemoryRepository interface {
	Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error)
	Save(ctx context.Context, proj *entity.CaseMemoryProjection) error
	Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error)
	// List returns all memory projections ordered by projection_version DESC
	// (P2 D15 export).
	List(ctx context.Context) ([]*entity.CaseMemoryProjection, error)
	Delete(ctx context.Context, caseID string) error
}

type ToolCallRepository interface {
	Create(ctx context.Context, t *entity.ToolCall) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error)
}
