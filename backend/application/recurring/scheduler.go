package recurring

import (
	"context"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

// Scheduler periodically ticks the recurring service. The ticker and context
// live for the process lifetime; cancel the context to stop.
type Scheduler struct {
	svc      *Service
	interval time.Duration
	lock     port.SchedulerLock
	owner    string
}

func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	return NewSchedulerWithLock(svc, interval, nil, "")
}

// NewSchedulerWithLock adds a distributed lease so only one replica ticks.
func NewSchedulerWithLock(svc *Service, interval time.Duration, lock port.SchedulerLock, owner string) *Scheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Scheduler{svc: svc, interval: interval, lock: lock, owner: owner}
}

const schedulerLockName = "recurring-tick"

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if s.lock == nil {
				_ = s.svc.Tick(ctx, time.Now())
				continue
			}
			ok, err := s.lock.Acquire(ctx, schedulerLockName, s.owner, s.interval*2)
			if err != nil || !ok {
				continue // another replica holds the lease
			}
			_ = s.svc.Tick(ctx, time.Now())
			_ = s.lock.Release(ctx, schedulerLockName, s.owner)
		case <-ctx.Done():
			if s.lock != nil {
				_ = s.lock.Release(ctx, schedulerLockName, s.owner)
			}
			return
		}
	}
}
