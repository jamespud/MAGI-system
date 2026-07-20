package server

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/sse"

	"github.com/jamespud/magi/backend/domain/entity"
)

// SSEHandler handles GET /api/v1/cases/:id/stream via Server-Sent Events.
// On connect it replays all already-stored events for the case (catch-up),
// then forwards live events. Duplicate IDs are skipped to defend against the
// race where an event is both in the history snapshot and the live channel.
func SSEHandler(broker *EventBroker) func(ctx context.Context, c *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		caseID := c.Param("id")
		ch, history := broker.SubscribeWithReplay(caseID)
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
	data, _ := json.Marshal(ev)
	return w.Publish(&sse.Event{
		ID:    ev.ID,
		Event: string(ev.Type),
		Data:  data,
	})
}
