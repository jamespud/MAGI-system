package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/server/dto"
)

type StatusHandler struct {
	reg       *metrics.Registry
	modelName string
}

func NewStatusHandler(reg *metrics.Registry, modelName string) *StatusHandler {
	return &StatusHandler{reg: reg, modelName: modelName}
}

// Status reports live harness state for the top navigation bar.
func (h *StatusHandler) Status(ctx context.Context, c *app.RequestContext) {
	resp := dto.StatusResponse{ModelName: h.modelName, Connected: true}
	if h.reg != nil {
		resp.TokensTotal = h.reg.TokensTotal.Load()
		resp.RunsActive = h.reg.RunsActive.Load()
		resp.CostUSD = float64(h.reg.CostTotalMicro.Load()) / 1e6
	}
	c.JSON(consts.StatusOK, resp)
}
