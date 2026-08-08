package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/server/dto"
)

type AdminHandler struct {
	svc *admin.Service
}

func NewAdminHandler(svc *admin.Service) *AdminHandler {
	return &AdminHandler{svc: svc}
}

func (h *AdminHandler) Usage(ctx context.Context, c *app.RequestContext) {
	sum, err := h.svc.Usage(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromAdminUsage(sum))
}
