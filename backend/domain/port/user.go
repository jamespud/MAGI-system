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

// UserRepository persists harness accounts.
type UserRepository interface {
	Create(ctx context.Context, u *entity.User) error
	GetByID(ctx context.Context, id int64) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	List(ctx context.Context) ([]*entity.User, error)
	Update(ctx context.Context, u *entity.User) error
	Delete(ctx context.Context, id int64) error
}

// AuthUserWriter applies account mutations while maintaining the session
// authorization version. Each versioned mutation must bump auth_version in the
// same statement that applies the change, so a concurrent read can never
// observe the new role/status with the old version. It is deliberately
// separate from UserRepository so read-only callers and fakes are unaffected.
type AuthUserWriter interface {
	// SetUserRole changes the role and returns the new auth_version.
	SetUserRole(ctx context.Context, id int64, role string) (int64, error)
	// SetUserStatus changes the account status and returns the new auth_version.
	SetUserStatus(ctx context.Context, id int64, status string) (int64, error)
	// BumpAuthVersion invalidates every existing session for the user.
	BumpAuthVersion(ctx context.Context, id int64) (int64, error)
	// UpdateUserProfile changes non-authorization profile fields WITHOUT
	// touching auth_version (name/email are not authorization facts).
	UpdateUserProfile(ctx context.Context, id int64, name, email string) error
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
