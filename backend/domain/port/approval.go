package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ApprovalRepository persists human-in-the-loop tool approval requests.
type ApprovalRepository interface {
	Create(ctx context.Context, a *entity.ApprovalRequest) error
	Get(ctx context.Context, id string) (*entity.ApprovalRequest, error)
	// FindByKey returns the latest request for a (case, run, tool) key so retries
	// reuse a decision instead of spamming the human with duplicate requests.
	FindByKey(ctx context.Context, caseID, runID, toolName string) (*entity.ApprovalRequest, error)
	List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error)
	ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error)
	Approve(ctx context.Context, id, decidedBy, reason string) error
	Reject(ctx context.Context, id, decidedBy, reason string) error
	MarkExpired(ctx context.Context, id string) error
}
