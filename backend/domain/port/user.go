package port

import (
	"context"
	"errors"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ErrUserNotFound is returned by user lookups when no account exists. It lets
// callers distinguish "deleted/absent" (deny the session) from a storage
// failure (fail closed without claiming the account is gone).
var ErrUserNotFound = errors.New("user not found")

// ErrAPIKeyNotFound is returned by key lookups when no key matches. It lets the
// authenticator distinguish "no such credential" (reject) from a key-store
// failure (fail closed).
var ErrAPIKeyNotFound = errors.New("api key not found")

// UserRepository persists harness accounts.
type UserRepository interface {
	Create(ctx context.Context, u *entity.User) error
	GetByID(ctx context.Context, id int64) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	List(ctx context.Context) ([]*entity.User, error)
	Update(ctx context.Context, u *entity.User) error
	Delete(ctx context.Context, id int64) error
}

// UserMutation is a fully normalized account patch applied in ONE statement.
// Nil fields are left unchanged. When BumpAuthVersion is set, auth_version is
// incremented by the same UPDATE, so no reader can observe the new
// authorization facts paired with the previous version.
type UserMutation struct {
	Name            *string
	Email           *string
	Role            *string
	Status          *string
	BumpAuthVersion bool
}

// AuthUserWriter applies account mutations while maintaining the session
// authorization version. It is deliberately separate from UserRepository so
// read-only callers and fakes are unaffected.
type AuthUserWriter interface {
	// ApplyUserMutation applies every populated field of m atomically and
	// returns the stored user afterwards.
	ApplyUserMutation(ctx context.Context, id int64, m UserMutation) (*entity.User, error)
	// BumpAuthVersion invalidates every existing session for the user.
	BumpAuthVersion(ctx context.Context, id int64) (int64, error)
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
