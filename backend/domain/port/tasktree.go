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
