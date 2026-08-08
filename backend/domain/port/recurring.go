package port

import (
	"context"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

// RecurringRepository persists recurring decision templates.
type RecurringRepository interface {
	Create(ctx context.Context, r *entity.RecurringCase) error
	Get(ctx context.Context, id string) (*entity.RecurringCase, error)
	ListByUser(ctx context.Context, userID int64) ([]*entity.RecurringCase, error)
	ListEnabled(ctx context.Context) ([]*entity.RecurringCase, error)
	Update(ctx context.Context, r *entity.RecurringCase) error
	UpdateEnabled(ctx context.Context, id string, enabled bool) error
	UpdateLastRun(ctx context.Context, id string, at time.Time) error
	Delete(ctx context.Context, id string) error
}
