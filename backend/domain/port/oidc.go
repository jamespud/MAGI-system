package port

import (
	"context"
	"time"
)

// OIDCStateRepository stores one-time OIDC authorization states. It exists so a
// provider callback can be validated by ANY replica: an in-process store fails
// whenever the callback lands on a different pod than the /login request.
type OIDCStateRepository interface {
	// Issue records a freshly generated state that expires at expiresAt.
	Issue(ctx context.Context, state string, expiresAt time.Time) error
	// Consume claims a state exactly once, returning false when it is unknown,
	// already used, or expired.
	Consume(ctx context.Context, state string) (bool, error)
}
