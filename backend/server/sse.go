package server

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/sse"
)

// SSEHandler handles GET /api/v1/cases/:id/stream via Server-Sent Events.
// It subscribes to the EventBroker for the case ID and forwards events
// to the client via Hertz SSE stream (synchronous within handler).
func SSEHandler(broker *EventBroker) func(ctx context.Context, c *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		caseID := c.Param("id")
		ch := broker.Subscribe(caseID)
		defer broker.Unsubscribe(caseID, ch)

		w := sse.NewStream(c)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(ev)
				if err := w.Publish(&sse.Event{
					ID:    ev.ID,
					Event: string(ev.Type),
					Data:  data,
				}); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}
