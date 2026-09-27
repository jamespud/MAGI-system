package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ApprovalRepository persists human-in-the-loop tool approval requests.
type ApprovalRepository interface {
	Create(ctx context.Context, a *entity.ApprovalRequest) error
	Get(ctx context.Context, id string) (*entity.ApprovalRequest, error)
	// FindByKey returns the latest request for the exact
	// (case, run, tool, intent digest) key so a safe retry of the same invocation
	// reuses a decision instead of spamming the human with duplicate requests.
	// Callers must pass the digest of the invocation they are about to run; a
	// different argument set, or a legacy row stored without a digest, must never
	// resolve to an existing decision.
	FindByKey(ctx context.Context, caseID, runID, toolName, intentDigest string) (*entity.ApprovalRequest, error)
	List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error)
	ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error)
	Approve(ctx context.Context, id, decidedBy, reason string) error
	Reject(ctx context.Context, id, decidedBy, reason string) error
	MarkExpired(ctx context.Context, id string) error
}
