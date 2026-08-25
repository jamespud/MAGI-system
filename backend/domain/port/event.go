package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// EventPublisher publishes MagiEvents to SSE + EventStore (ADR-008).
type EventPublisher interface {
	Publish(ctx context.Context, e entity.MagiEvent) error
}

// LiveEventPublisher forwards an event that is already durable. It lets a
// transactional writer commit an event first and then notify live listeners
// without attempting a second persistence write.
type LiveEventPublisher interface {
	PublishLive(ctx context.Context, e entity.MagiEvent) error
}
