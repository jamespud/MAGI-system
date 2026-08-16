package server

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/server/dto"
)

// RateLimitConfig mirrors bootstrap.HTTPRateLimit (kept decoupled so the
// middleware stays testable without the full config type).
type RateLimitConfig struct {
	Enabled          bool
	PerUserPerMinute int
	PerIPPerMinute   int
}

// rateWindow is a fixed one-minute window counter. The window is reset lazily
// when the key is observed in a new minute.
type rateWindow struct {
	count int
	reset time.Time
}

// RateLimiter is an in-memory, per-key sliding window limiter. It is
// intentionally single-instance (per-process); for a multi-instance limit,
// the DB-backed run concurrency and tool quotas already provide cross-replica
// governance. The map is pruned opportunistically to bound memory.
type RateLimiter struct {
	mu      sync.Mutex
	user    map[int64]*rateWindow
	ip      map[string]*rateWindow
	perUser int
	perIP   int
	now     func() time.Time
}

// NewRateLimiter builds a limiter with the given per-minute budgets. A
// non-positive budget disables that dimension.
func NewRateLimiter(perUser, perIP int) *RateLimiter {
	return &RateLimiter{
		user:    make(map[int64]*rateWindow),
		ip:      make(map[string]*rateWindow),
		perUser: perUser,
		perIP:   perIP,
		now:     time.Now,
	}
}

// Allow reports whether the key is within budget, and the number of seconds
// until the window resets (for Retry-After).
func (l *RateLimiter) Allow(userID int64, ip string) (ok bool, retryAfter int) {
	if l.perUser <= 0 && l.perIP <= 0 {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	best := 60
	if l.perUser > 0 && userID != 0 {
		if ok, sec := allowWindow(l.user, userID, l.perUser, now); !ok {
			return false, sec
		} else if sec < best {
			best = sec
		}
	}
	if l.perIP > 0 && ip != "" {
		if ok, sec := allowWindow(l.ip, ip, l.perIP, now); !ok {
			return false, sec
		} else if sec < best {
			best = sec
		}
	}
	// Opportunistic prune: drop entries older than two windows.
	for k, w := range l.user {
		if now.Sub(w.reset) > 2*time.Minute {
			delete(l.user, k)
		}
	}
	for k, w := range l.ip {
		if now.Sub(w.reset) > 2*time.Minute {
			delete(l.ip, k)
		}
	}
	return true, best
}

// allowWindow is called with l.mu held. It is a package-level generic
// function because Go does not allow type parameters on methods.
func allowWindow[K comparable](m map[K]*rateWindow, key K, budget int, now time.Time) (bool, int) {
	w := m[key]
	if w == nil || now.Sub(w.reset) >= time.Minute {
		w = &rateWindow{count: 0, reset: now.Add(time.Minute)}
		m[key] = w
	}
	if w.count >= budget {
		return false, int(time.Until(w.reset).Seconds()) + 1
	}
	w.count++
	return true, 0
}

// RateLimit returns middleware that enforces the configured per-minute limits
// keyed by authenticated user id, falling back to client IP in open mode. It
// must run after Auth so the principal is available.
func RateLimit(cfg RateLimitConfig) app.HandlerFunc {
	if !cfg.Enabled || (cfg.PerUserPerMinute <= 0 && cfg.PerIPPerMinute <= 0) {
		return func(ctx context.Context, c *app.RequestContext) { c.Next(ctx) }
	}
	lim := NewRateLimiter(cfg.PerUserPerMinute, cfg.PerIPPerMinute)
	return func(ctx context.Context, c *app.RequestContext) {
		userID := int64(0)
		if p := auth.PrincipalFrom(ctx); p != nil {
			userID = p.UserID
		}
		ip := c.ClientIP()
		ok, retryAfter := lim.Allow(userID, ip)
		if !ok {
			c.Header("Retry-After", retryAfterHeader(retryAfter))
			c.AbortWithStatusJSON(consts.StatusTooManyRequests, dto.ErrorResponse{
				Error: "rate limit exceeded",
			})
			return
		}
		c.Next(ctx)
	}
}

func retryAfterHeader(sec int) string {
	if sec < 1 {
		return "1"
	}
	return itoa(sec)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
