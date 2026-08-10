package handler

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/auth"
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
		// Scoped to the authenticated caller: multi-tenant status must not leak
		// other users' usage or the instance-wide cost picture.
		userID := auth.PrincipalFrom(ctx)
		uid := ""
		if userID != nil {
			uid = fmt.Sprintf("%d", userID.UserID)
		}
		resp.TokensTotal, resp.CostUSD, resp.RunsActive = h.reg.UserStatus(uid)
	}
	c.JSON(consts.StatusOK, resp)
}
