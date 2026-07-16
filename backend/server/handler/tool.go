package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/server/dto"
)

type ToolHandler struct {
	svc *tool.Service
}

func NewToolHandler(svc *tool.Service) *ToolHandler {
	return &ToolHandler{svc: svc}
}

func (h *ToolHandler) List(ctx context.Context, c *app.RequestContext) {
	defs, err := h.svc.List(ctx, nil)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ToolResponse, 0, len(defs))
	for _, d := range defs {
		out = append(out, dto.FromTool(d))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *ToolHandler) Get(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	def, err := h.svc.Get(ctx, name)
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromTool(*def))
}
