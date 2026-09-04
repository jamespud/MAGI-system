package execution

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// EventRecorder separates durable execution-history events (which must fail
// closed and stop the run) from best-effort telemetry events (which must never
// stop a run). Both share the same durable event store and sequence ordering;
// only the error handling differs.
//
// The name is distinct from kernel.Recorder: EventRecorder records high-level
// MAGI runtime events, while package-level Recorder records invocation
// transitions for the execution kernel.
type EventRecorder interface {
	Critical(ctx context.Context, event entity.MagiEvent) error
	Telemetry(ctx context.Context, event entity.MagiEvent)
}

// PublisherEventRecorder is the default EventRecorder backed by a
// port.EventPublisher. A critical event surfaces the publisher error so the
// caller can stop before any external side effect; a telemetry event swallows
// the error on purpose.
type PublisherEventRecorder struct {
	pub port.EventPublisher
}

func NewEventRecorder(pub port.EventPublisher) *PublisherEventRecorder {
	return &PublisherEventRecorder{pub: pub}
}

func (r *PublisherEventRecorder) Critical(ctx context.Context, event entity.MagiEvent) error {
	if r == nil || r.pub == nil {
		return nil
	}
	return r.pub.Publish(ctx, event)
}

func (r *PublisherEventRecorder) Telemetry(ctx context.Context, event entity.MagiEvent) {
	if r == nil || r.pub == nil {
		return
	}
	_ = r.pub.Publish(ctx, event)
}

var _ EventRecorder = (*PublisherEventRecorder)(nil)
