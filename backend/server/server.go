package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// RegisterRoutes registers all HTTP routes on the Hertz server.
// Phase 1: only /health. Phase 3 adds full API.
func RegisterRoutes(h *hzserver.Hertz) {
	h.GET("/health", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
	})
}
