package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Pinger checks a dependency (e.g. the database) for readiness.
type Pinger func(ctx context.Context) error

type HealthHandler struct {
	pinger Pinger
}

func NewHealthHandler(pingers ...Pinger) *HealthHandler {
	var p Pinger
	if len(pingers) > 0 {
		p = pingers[0]
	}
	return &HealthHandler{pinger: p}
}

func (h *HealthHandler) Health(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) Ready(ctx context.Context, c *app.RequestContext) {
	if h.pinger != nil {
		if err := h.pinger(ctx); err != nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"status": "unready", "error": err.Error()})
			return
		}
	}
	c.JSON(consts.StatusOK, map[string]string{"status": "ready"})
}

func (h *HealthHandler) Version(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"version": "2.0.0"})
}
