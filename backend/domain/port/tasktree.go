package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// TaskTreeRepository persists the task state tree for a case.
type TaskTreeRepository interface {
	RecordAgent(ctx context.Context, caseID, runID, agentCode, status string) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.TaskNode, error)
}

// TaskTreeRecorder is the read-only write surface consumed by the agent loop.
type TaskTreeRecorder interface {
	RecordAgent(ctx context.Context, caseID, runID, agentCode, status string) error
}

// TaskTreeCleaner is an optional TaskTreeRepository capability that removes a
// case's task nodes. Used to reset the tree on re-run and to cascade-delete
// nodes when a case is removed.
type TaskTreeCleaner interface {
	DeleteByCase(ctx context.Context, caseID string) error
}
