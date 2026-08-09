package port

import (
	"context"
	"time"
)

// ToolQuotaRepository persists per-user per-tool usage counters for sliding
// rate windows. TryConsume returns true when the call is allowed.
type ToolQuotaRepository interface {
	TryConsume(ctx context.Context, userID int64, toolName string, windowStart time.Time, limit int) (bool, error)
}

// ToolQuotaPort is the runtime-facing quota gate.
type ToolQuotaPort interface {
	Allow(ctx context.Context, userID string, toolName string) (bool, error)
}
