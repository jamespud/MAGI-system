package magi

import (
	"context"
	"encoding/json"
	"sync"

	hertzsse "github.com/hertz-contrib/sse"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/coze-dev/coze-studio/backend/infra/sse"
)

// EventPublisherAdapter implements port.EventPublisher (ADR-008).
// Dual-publish: EventStore (persistence) + SSE (real-time push, optional).
type EventPublisherAdapter struct {
	store  port.EventRepository
	sender sse.SSender   // optional, nil = no SSE
	stream *hertzsse.Stream // optional, nil = no SSE
}

func NewEventPublisherAdapter(store port.EventRepository) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store}
}

func NewEventPublisherAdapterWithSSE(store port.EventRepository, sender sse.SSender, stream *hertzsse.Stream) *EventPublisherAdapter {
	return &EventPublisherAdapter{store: store, sender: sender, stream: stream}
}

func (p *EventPublisherAdapter) Publish(ctx context.Context, e entity.MagiEvent) error {
	if p.store != nil {
		_ = p.store.Create(ctx, &e) // non-blocking; events don't fail the run
	}
	if p.sender != nil && p.stream != nil {
		data, _ := json.Marshal(e)
		_ = p.sender.Send(ctx, p.stream, &hertzsse.Event{Event: string(e.Type), Data: data})
	}
	return nil
}

var _ port.EventPublisher = (*EventPublisherAdapter)(nil)

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
	r.events[e.CaseID] = append(r.events[e.CaseID], e)
	return nil
}

func (r *InMemoryEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[caseID], nil
}

var _ port.EventRepository = (*InMemoryEventRepo)(nil)
