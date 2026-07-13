package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// EventPublisher publishes MagiEvents to SSE + EventStore (ADR-008).
type EventPublisher interface {
	Publish(ctx context.Context, e entity.MagiEvent) error
}
