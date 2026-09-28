package port

import (
	"context"
	"errors"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ErrApprovalConflict reports that another writer already stored the
// authoritative request for the same logical invocation. The caller must
// re-read that request; a second request for one invocation must never exist.
var ErrApprovalConflict = errors.New("approval request already exists for this invocation")

// ApprovalRepository persists human-in-the-loop tool approval requests.
type ApprovalRepository interface {
	Create(ctx context.Context, a *entity.ApprovalRequest) error
	Get(ctx context.Context, id string) (*entity.ApprovalRequest, error)
	// FindByInvocation returns the authoritative request for one logical
	// invocation so a resumed or retried attempt reuses its decision instead of
	// asking the human again. The invocation id already scopes the run, so this
	// deliberately does not compare physical run ids: a dispatcher retry
	// re-enters with a different run id for the same logical call. A different
	// invocation, or a legacy row stored without an invocation, must never
	// resolve here.
	FindByInvocation(ctx context.Context, caseID, invocationID string) (*entity.ApprovalRequest, error)
	List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error)
	ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error)
	Approve(ctx context.Context, id, decidedBy, reason string) error
	Reject(ctx context.Context, id, decidedBy, reason string) error
	MarkExpired(ctx context.Context, id string) error
}
