package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/hertz-contrib/sse"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/dto"
	"github.com/jamespud/magi/backend/server/handler"
)

// crossInstancePollInterval is how often an SSE subscriber polls the shared
// event store for events persisted by other worker replicas. The in-process
// broker covers local events immediately; the poller bridges replicas with
// bounded latency without requiring a dedicated pub/sub broker.
var crossInstancePollInterval = 2 * time.Second

// SSEHandler handles GET /api/v1/cases/:id/stream via Server-Sent Events.
// On connect it replays all already-stored events for the case (catch-up),
// then forwards live events. Duplicate IDs are skipped to defend against the
// race where an event is both in the history snapshot and the live channel.
func SSEHandler(broker *EventBroker) func(ctx context.Context, c *app.RequestContext) {
	return SSEHandlerWithHistory(broker, nil, nil)
}

// SSEHandlerWithHistory replays persisted events before forwarding live ones,
// and enforces case ownership when a case getter is provided.
//
// Delivery has two sources:
//  1. The in-process EventBroker channel: immediate local events.
//  2. A DB poller (when an EventRepository is wired): events persisted by
//     other worker instances that this process's broker never sees. Polling
//     the shared event store makes live streaming work across replicas.
//
// Events from both sources are de-duplicated by ID, so an event that is both
// in the local channel and in the polled history is delivered exactly once.
func SSEHandlerWithHistory(broker *EventBroker, repo port.EventRepository, caseSvc handler.CaseGetter) func(ctx context.Context, c *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		caseID := c.Param("id")
		if caseSvc != nil {
			case_, _ := caseSvc.Get(ctx, caseID)
			if !handler.CaseAllowed(ctx, case_) {
				handler.Forbidden(c)
				return
			}
		}
		var ch chan *entity.MagiEvent
		var history []*entity.MagiEvent
		if repo == nil {
			ch, history = broker.SubscribeWithReplay(caseID)
		} else {
			ch = broker.Subscribe(caseID)
			history, _ = repo.ListByCase(ctx, caseID)
		}
		defer broker.Unsubscribe(caseID, ch)

		w := sse.NewStream(c)
		replay := newSSEReplay(history)

		for _, ev := range history {
			if err := writeEvent(w, ev); err != nil {
				return
			}
		}

		// Cross-instance poller. A nil poll channel blocks forever in select,
		// preserving the pure in-memory behavior when no repo is wired.
		var pollC <-chan time.Time
		var ticker *time.Ticker
		if repo != nil {
			ticker = time.NewTicker(crossInstancePollInterval)
			defer ticker.Stop()
			pollC = ticker.C
		}

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if !replay.forward(w, ev) {
					return
				}
			case <-pollC:
				if err := pollOnce(ctx, repo, caseID, replay, func(ev *entity.MagiEvent) error {
					return writeEvent(w, ev)
				}); err != nil {
					continue // transient store error; retry on the next tick
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// sseReplay tracks the events already delivered to one SSE connection so
// overlapping delivery sources (history, local channel, DB poller) never
// send the same event twice, and tracks the watermark for ListAfter.
type sseReplay struct {
	sent   map[string]struct{}
	lastTS time.Time
}

func newSSEReplay(history []*entity.MagiEvent) *sseReplay {
	r := &sseReplay{sent: make(map[string]struct{}, len(history))}
	for _, ev := range history {
		r.record(ev)
	}
	return r
}

// record marks an event as delivered and advances the timestamp watermark.
func (r *sseReplay) record(ev *entity.MagiEvent) {
	if ev == nil {
		return
	}
	if ev.ID != "" {
		r.sent[ev.ID] = struct{}{}
	}
	if ev.Timestamp.After(r.lastTS) {
		r.lastTS = ev.Timestamp
	}
}

// isNew reports whether an event has not yet been delivered.
func (r *sseReplay) isNew(ev *entity.MagiEvent) bool {
	if ev == nil {
		return false
	}
	if ev.ID == "" {
		return true
	}
	_, dup := r.sent[ev.ID]
	return !dup
}

// forward writes an event unless it was already delivered. It returns false
// when the write failed (client gone), terminating the stream.
func (r *sseReplay) forward(w *sse.Stream, ev *entity.MagiEvent) bool {
	if !r.isNew(ev) {
		return true
	}
	r.record(ev)
	return writeEvent(w, ev) == nil
}

// pollOnce fetches events persisted since the watermark and emits each new
// one via emit. It advances the watermark as it goes. Transient store errors
// are returned to the caller, which retries on the next poll tick.
func pollOnce(ctx context.Context, repo port.EventRepository, caseID string, replay *sseReplay, emit func(*entity.MagiEvent) error) error {
	polled, err := repo.ListAfter(ctx, caseID, replay.lastTS)
	if err != nil {
		return err
	}
	for _, ev := range polled {
		if !replay.isNew(ev) {
			replay.record(ev)
			continue
		}
		replay.record(ev)
		if err := emit(ev); err != nil {
			return err
		}
	}
	return nil
}

func writeEvent(w *sse.Stream, ev *entity.MagiEvent) error {
	// Serialize through the DTO so SSE frames carry the same snake_case shape
	// (id/type/agent_code/run_id/message/payload/timestamp) as the REST event
	// endpoint. Marshalling the raw entity would emit Go field names
	// (ID/CaseID/AgentCode/...) that the frontend EventSource cannot parse.
	data, _ := json.Marshal(dto.FromEvent(ev))
	// Deliberately leave Event empty: an `event:` line makes the browser
	// dispatch a named event that es.onmessage does not receive. A frame
	// without `event:` dispatches "message", which is what the frontend
	// subscribes to.
	return w.Publish(&sse.Event{
		ID:   ev.ID,
		Data: data,
	})
}
