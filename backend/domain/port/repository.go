package port

import (
	"context"

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
}

type CaseRepository interface {
	Create(ctx context.Context, c *entity.DecisionCase) error
	Get(ctx context.Context, id string) (*entity.DecisionCase, error)
	List(ctx context.Context) ([]*entity.DecisionCase, error)
	UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error
}

type AgentRunRepository interface {
	Create(ctx context.Context, r *entity.AgentRun) error
	Get(ctx context.Context, id string) (*entity.AgentRun, error)
	ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error)
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
}

type CheckpointRepository interface {
	Save(ctx context.Context, s *entity.AgentState) error
	Load(ctx context.Context, runID string) (*entity.AgentState, error)
}

type MemoryRepository interface {
	Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error)
	Save(ctx context.Context, proj *entity.CaseMemoryProjection) error
}

type ToolCallRepository interface {
	Create(ctx context.Context, t *entity.ToolCall) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error)
}
