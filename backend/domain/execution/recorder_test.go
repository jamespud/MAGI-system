package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
)

type recorderPublisher struct {
	err   error
	calls []entity.EventType
}

func (p *recorderPublisher) Publish(_ context.Context, e entity.MagiEvent) error {
	p.calls = append(p.calls, e.Type)
	return p.err
}

var _ port.EventPublisher = (*recorderPublisher)(nil)

func TestRecorderCriticalReturnsPublisherError(t *testing.T) {
	want := errors.New("event store down")
	pub := &recorderPublisher{err: want}
	rec := execution.NewEventRecorder(pub)

	ev := entity.NewEvent("case-1", "run-1", nil, entity.EventInvocationStarted, nil)
	if err := rec.Critical(context.Background(), ev); !errors.Is(err, want) {
		t.Fatalf("Critical error = %v, want %v", err, want)
	}
}

func TestRecorderTelemetrySwallowsPublisherError(t *testing.T) {
	pub := &recorderPublisher{err: errors.New("event store down")}
	rec := execution.NewEventRecorder(pub)

	ev := entity.NewEvent("case-1", "run-1", nil, entity.EventModelResponded, nil)
	// Telemetry has no error return; a publisher failure must not surface or
	// otherwise stop the caller.
	rec.Telemetry(context.Background(), ev)
	if len(pub.calls) != 1 || pub.calls[0] != entity.EventModelResponded {
		t.Fatalf("telemetry publisher calls = %v, want [MODEL_RESPONDED]", pub.calls)
	}
}

func TestRecorderNilPublisherIsSafe(t *testing.T) {
	rec := execution.NewEventRecorder(nil)
	if err := rec.Critical(context.Background(), entity.MagiEvent{}); err != nil {
		t.Fatalf("Critical with nil publisher: %v", err)
	}
	rec.Telemetry(context.Background(), entity.MagiEvent{})
}
