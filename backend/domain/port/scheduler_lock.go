package port

import (
	"context"
	"time"
)

// SchedulerLock is a distributed lease so only one replica runs the recurring
// scheduler at a time. Acquire returns true only when the caller holds the
// lease (fresh or renewed); Release surrenders it.
type SchedulerLock interface {
	Acquire(ctx context.Context, name, owner string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, name, owner string) error
}
