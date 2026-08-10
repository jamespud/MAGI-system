package port

import "context"

// RunCounter enforces per-user concurrent run limits atomically across
// replicas. Implementations back the counter with a shared table so two
// instances cannot both pass the limit (check-and-increment is one statement).
type RunCounter interface {
	// Acquire atomically increments the user's active count only when it is
	// below limit. Returns true when the slot was taken.
	Acquire(ctx context.Context, userID int64, limit int) (bool, error)
	// Release decrements the user's active count. Called exactly once per
	// successful Acquire.
	Release(ctx context.Context, userID int64) error
}
