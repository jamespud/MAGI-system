package magi

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coze-dev/coze-studio/backend/infra/sse"
	hertzsse "github.com/hertz-contrib/sse"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
)

// EventPublisherAdapter implements port.EventPublisher (ADR-008).
// Dual-publish: EventStore (persistence) + SSE (real-time push, optional).
type EventPublisherAdapter struct {
	store    port.EventRepository
	live     port.EventPublisher // optional fan-out target, e.g. the SSE broker
	sender   sse.SSender         // optional, nil = no SSE
	stream   *hertzsse.Stream    // optional, nil = no SSE
	redactor *redact.Redactor    // optional, nil = no redaction
}

func NewEventPublisherAdapter(store port.EventRepository) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store}
}

// NewEventPublisherAdapterWithFanout persists events and then forwards the
// same event to a live subscriber transport. Keeping both responsibilities in
// one publisher prevents the production path from accidentally using an
// in-memory-only event store.
func NewEventPublisherAdapterWithFanout(store port.EventRepository, live port.EventPublisher) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store, live: live}
}

// NewEventPublisherAdapterWithRedaction persists redacted events and forwards
// them to a live transport. Known secrets are masked before anything is stored
// or pushed.
func NewEventPublisherAdapterWithRedaction(store port.EventRepository, live port.EventPublisher, redactor *redact.Redactor) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store, live: live, redactor: redactor}
}

func NewEventPublisherAdapterWithSSE(store port.EventRepository, sender sse.SSender, stream *hertzsse.Stream) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store, sender: sender, stream: stream}
}

func (p *EventPublisherAdapter) Publish(ctx context.Context, e entity.MagiEvent) error {
	if p.redactor != nil {
		e.Payload = p.redactor.JSON(e.Payload)
	}
	var storeErr error
	if p.store != nil {
		storeErr = p.store.Create(ctx, &e)
		if storeErr != nil {
			return storeErr
		}
	}
	_ = p.publishLive(ctx, e)
	return storeErr
}

// PublishLive forwards an event that was persisted by a separate transaction.
// TerminalCommitter uses this to preserve atomic DB ownership fencing.
func (p *EventPublisherAdapter) PublishLive(ctx context.Context, e entity.MagiEvent) error {
	if p.redactor != nil {
		e.Payload = p.redactor.JSON(e.Payload)
	}
	return p.publishLive(ctx, e)
}

func (p *EventPublisherAdapter) publishLive(ctx context.Context, e entity.MagiEvent) error {
	if p.live != nil {
		if live, ok := p.live.(port.LiveEventPublisher); ok {
			if err := live.PublishLive(ctx, e); err != nil {
				return err
			}
		} else {
			if err := p.live.Publish(ctx, e); err != nil {
				return err
			}
		}
	}
	if p.sender != nil && p.stream != nil {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := p.sender.Send(ctx, p.stream, &hertzsse.Event{Event: string(e.Type), Data: data}); err != nil {
			return err
		}
	}
	return nil
}

// Critical implements execution.EventRecorder by surfacing the durable store
// error. The adapter already returns only the durable-write error from Publish
// (live/SSE fanout is best effort), so a critical event fails closed.
func (p *EventPublisherAdapter) Critical(ctx context.Context, e entity.MagiEvent) error {
	return p.Publish(ctx, e)
}

// Telemetry implements execution.EventRecorder as fire-and-forget. A telemetry
// event must never stop a run, even when the durable store is temporarily
// unavailable.
func (p *EventPublisherAdapter) Telemetry(ctx context.Context, e entity.MagiEvent) {
	_ = p.Publish(ctx, e)
}

var _ port.EventPublisher = (*EventPublisherAdapter)(nil)
var _ port.LiveEventPublisher = (*EventPublisherAdapter)(nil)
var _ execution.EventRecorder = (*EventPublisherAdapter)(nil)

// InMemoryEventRepo is a test/in-memory EventRepository.
type InMemoryEventRepo struct {
	mu     sync.Mutex
	events map[string][]*entity.MagiEvent
}

func NewInMemoryEventRepo() *InMemoryEventRepo {
	return &InMemoryEventRepo{events: make(map[string][]*entity.MagiEvent)}
}

func (r *InMemoryEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Seq == 0 {
		var maxSeq uint64
		for _, stored := range r.events[e.CaseID] {
			if stored.Seq > maxSeq {
				maxSeq = stored.Seq
			}
		}
		e.Seq = maxSeq + 1
	}
	r.events[e.CaseID] = append(r.events[e.CaseID], e)
	return nil
}

func (r *InMemoryEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[caseID], nil
}

func (r *InMemoryEventRepo) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*entity.MagiEvent
	for _, e := range r.events[caseID] {
		if !e.Timestamp.Before(after) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *InMemoryEventRepo) ListAfterSeq(ctx context.Context, caseID string, afterSeq uint64, limit int) ([]*entity.MagiEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var out []*entity.MagiEvent
	for _, e := range r.events[caseID] {
		if e.Seq > afterSeq {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

var _ port.EventRepository = (*InMemoryEventRepo)(nil)
