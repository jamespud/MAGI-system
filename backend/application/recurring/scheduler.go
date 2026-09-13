package recurring

import (
	"context"
	"log"
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
			// The lease is intentionally NOT released per tick: the same owner
			// renews it on the next Acquire, while another replica can only take
			// over once the TTL expires (owner died). Releasing after every tick
			// reopened a window where two replicas could both see a template as
			// due (see docs/reliability-hazard-audit.md §4.5).
			if err := s.svc.Tick(ctx, time.Now()); err != nil {
				log.Printf("recurring: tick failed: %v", err)
			}
		case <-ctx.Done():
			if s.lock != nil {
				_ = s.lock.Release(ctx, schedulerLockName, s.owner)
			}
			return
		}
	}
}
