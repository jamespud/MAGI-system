package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// UserRepository persists harness accounts.
type UserRepository interface {
	Create(ctx context.Context, u *entity.User) error
	GetByID(ctx context.Context, id int64) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	List(ctx context.Context) ([]*entity.User, error)
	Update(ctx context.Context, u *entity.User) error
	Delete(ctx context.Context, id int64) error
}

// ApiKeyRepository persists DB-backed API keys (hash-only).
type ApiKeyRepository interface {
	Create(ctx context.Context, k *entity.ApiKey) error
	GetByID(ctx context.Context, id string) (*entity.ApiKey, error)
	ListByUser(ctx context.Context, userID int64) ([]*entity.ApiKey, error)
	FindByKeyHash(ctx context.Context, hash string) (*entity.ApiKey, error)
	Update(ctx context.Context, k *entity.ApiKey) error
	Delete(ctx context.Context, id string) error
}
