package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// AuditRepository persists the administrative/security audit trail.
type AuditRepository interface {
	Record(ctx context.Context, e *entity.AuditEvent) error
	List(ctx context.Context, limit, offset int) ([]*entity.AuditEvent, int64, error)
}
