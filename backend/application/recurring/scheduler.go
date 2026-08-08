package recurring

import (
	"context"
	"time"
)

// Scheduler periodically ticks the recurring service. The ticker and context
// live for the process lifetime; cancel the context to stop.
type Scheduler struct {
	svc      *Service
	interval time.Duration
}

func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Scheduler{svc: svc, interval: interval}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = s.svc.Tick(ctx, time.Now())
		case <-ctx.Done():
			return
		}
	}
}
