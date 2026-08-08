package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// PluginBindingRepository persists user-scoped plugin tool bindings.
type PluginBindingRepository interface {
	Create(ctx context.Context, b *entity.PluginBinding) error
	Get(ctx context.Context, id string) (*entity.PluginBinding, error)
	ListByUser(ctx context.Context, userID int64) ([]*entity.PluginBinding, error)
	UpdateEnabled(ctx context.Context, id string, enabled bool) error
	Delete(ctx context.Context, id string) error
}
