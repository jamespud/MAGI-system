package server

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/hertz-contrib/sse"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/dto"
	"github.com/jamespud/magi/backend/server/handler"
)

// SSEHandler handles GET /api/v1/cases/:id/stream via Server-Sent Events.
// On connect it replays all already-stored events for the case (catch-up),
// then forwards live events. Duplicate IDs are skipped to defend against the
// race where an event is both in the history snapshot and the live channel.
func SSEHandler(broker *EventBroker) func(ctx context.Context, c *app.RequestContext) {
	return SSEHandlerWithHistory(broker, nil, nil)
}

// SSEHandlerWithHistory replays persisted events before forwarding live ones,
// and enforces case ownership when a case getter is provided.
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
		sent := make(map[string]struct{}, len(history))

		for _, ev := range history {
			if err := writeEvent(w, ev); err != nil {
				return
			}
			if ev.ID != "" {
				sent[ev.ID] = struct{}{}
			}
		}

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.ID != "" {
					if _, dup := sent[ev.ID]; dup {
						continue
					}
					sent[ev.ID] = struct{}{}
				}
				if err := writeEvent(w, ev); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

func writeEvent(w *sse.Stream, ev *entity.MagiEvent) error {
	// Serialize through the DTO so SSE frames carry the same snake_case shape
	// (id/type/agent_code/run_id/message/payload/timestamp) as the REST event
	// endpoint. Marshalling the raw entity would emit Go field names
	// (ID/CaseID/AgentCode/...) that the frontend EventSource cannot parse.
	data, _ := json.Marshal(dto.FromEvent(ev))
	return w.Publish(&sse.Event{
		ID:    ev.ID,
		Event: string(ev.Type),
		Data:  data,
	})
}
