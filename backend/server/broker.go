package server

import (
	"context"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// EventBroker is a channel-based event broker that implements both
// port.EventPublisher and port.EventRepository. It bridges async event
// publishing with sync SSE consumption (§8).
type EventBroker struct {
	mu          sync.Mutex
	subscribers map[string][]chan *entity.MagiEvent
	stored      map[string][]*entity.MagiEvent
	bufferSize  int
	dropped     atomic.Int64
}

func NewEventBroker() *EventBroker {
	return NewEventBrokerWithBuffer(64)
}

// NewEventBrokerWithBuffer builds a broker with a custom per-subscriber
// channel buffer (exposed for tests).
func NewEventBrokerWithBuffer(bufferSize int) *EventBroker {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	return &EventBroker{
		subscribers: make(map[string][]chan *entity.MagiEvent),
		stored:      make(map[string][]*entity.MagiEvent),
		bufferSize:  bufferSize,
	}
}

func (b *EventBroker) Publish(ctx context.Context, e entity.MagiEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.Seq == 0 {
		var maxSeq uint64
		for _, stored := range b.stored[e.CaseID] {
			if stored.Seq > maxSeq {
				maxSeq = stored.Seq
			}
		}
		e.Seq = maxSeq + 1
	}
	b.stored[e.CaseID] = append(b.stored[e.CaseID], &e)
	b.publishLiveLocked(e)
	return nil
}

// PublishLive fans out an event that another component has already persisted.
// It intentionally does not append to stored: durable replay remains owned by
// the transactional event repository.
func (b *EventBroker) PublishLive(ctx context.Context, e entity.MagiEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishLiveLocked(e)
	return nil
}

func (b *EventBroker) publishLiveLocked(e entity.MagiEvent) {
	for _, ch := range b.subscribers[e.CaseID] {
		select {
		case ch <- &e:
		default:
			// Slow subscriber: the event is still persisted in b.stored (and
			// the DB), so a reconnect/replay can recover it. Count the drop
			// so the frontend's sequence-gap detection triggers a refetch.
			b.dropped.Add(1)
			log.Printf("event broker: dropped event %s for case %s (subscriber buffer full)", e.ID, e.CaseID)
		}
	}
}

// Dropped reports how many events were dropped for slow subscribers since
// startup. Used by tests and observability.
func (b *EventBroker) Dropped() int64 {
	if b == nil {
		return 0
	}
	return b.dropped.Load()
}

func (b *EventBroker) Subscribe(caseID string) chan *entity.MagiEvent {
	ch := make(chan *entity.MagiEvent, b.bufferSize)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[caseID] = append(b.subscribers[caseID], ch)
	return ch
}

// SubscribeWithReplay returns a live subscriber channel plus the snapshot of
// events already stored for caseID at subscribe time. Callers should flush the
// snapshot to the client first, then consume the channel, skipping any live
// event whose ID already appears in the snapshot (race protection against an
// event that is both in the history snapshot and the live channel buffer).
func (b *EventBroker) SubscribeWithReplay(caseID string) (chan *entity.MagiEvent, []*entity.MagiEvent) {
	ch := b.Subscribe(caseID)
	b.mu.Lock()
	defer b.mu.Unlock()
	stored := b.stored[caseID]
	history := make([]*entity.MagiEvent, len(stored))
	copy(history, stored)
	sort.SliceStable(history, func(i, j int) bool { return history[i].Seq < history[j].Seq })
	return ch, history
}

func (b *EventBroker) Unsubscribe(caseID string, ch chan *entity.MagiEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[caseID]
	for i, s := range subs {
		if s == ch {
			close(s)
			b.subscribers[caseID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (b *EventBroker) Create(ctx context.Context, e *entity.MagiEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.Seq == 0 {
		var maxSeq uint64
		for _, stored := range b.stored[e.CaseID] {
			if stored.Seq > maxSeq {
				maxSeq = stored.Seq
			}
		}
		e.Seq = maxSeq + 1
	}
	b.stored[e.CaseID] = append(b.stored[e.CaseID], e)
	return nil
}

func (b *EventBroker) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	events := b.stored[caseID]
	out := make([]*entity.MagiEvent, len(events))
	copy(out, events)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (b *EventBroker) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*entity.MagiEvent
	for _, e := range b.stored[caseID] {
		if !e.Timestamp.Before(after) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, nil
}

func (b *EventBroker) ListAfterSeq(ctx context.Context, caseID string, afterSeq uint64, limit int) ([]*entity.MagiEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var out []*entity.MagiEvent
	for _, e := range b.stored[caseID] {
		if e.Seq > afterSeq {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var _ port.EventPublisher = (*EventBroker)(nil)
var _ port.LiveEventPublisher = (*EventBroker)(nil)
var _ port.EventRepository = (*EventBroker)(nil)
