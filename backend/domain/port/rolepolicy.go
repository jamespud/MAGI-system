package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// RolePolicyRepository persists editable role-contract specifications. When a
// role has no stored spec the built-in default is used.
type RolePolicyRepository interface {
	Get(ctx context.Context, code string) (*entity.RolePolicy, error)
	Save(ctx context.Context, code string, p entity.RolePolicy) error
}
