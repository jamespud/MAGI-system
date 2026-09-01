package toolruntime

import (
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
)

func retrySafety(effect port.ToolEffectClass) execution.RetrySafety {
	switch effect {
	case port.ToolEffectReadOnly:
		return execution.RetrySafeReadOnly
	case port.ToolEffectIdempotent:
		return execution.RetrySafeIdempotent
	case port.ToolEffectNonIdempotent, port.ToolEffectUnknown, "":
		return execution.RetryUnsafe
	default:
		return execution.RetryUnsafe
	}
}
