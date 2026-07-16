package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) Health(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) Ready(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ready"})
}

func (h *HealthHandler) Version(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"version": "2.0.0"})
}
