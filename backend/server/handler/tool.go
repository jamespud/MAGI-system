package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type ToolHandler struct {
	svc   *tool.Service
	plugs *plugins.Service
}

func NewToolHandler(svc *tool.Service, plugs ...*plugins.Service) *ToolHandler {
	h := &ToolHandler{svc: svc}
	if len(plugs) > 0 {
		h.plugs = plugs[0]
	}
	return h
}

func (h *ToolHandler) List(ctx context.Context, c *app.RequestContext) {
	// Scope to the caller's enabled bindings: an authenticated user must not
	// see every server-configured MCP tool / sandbox capability.
	bindings, err := h.bindings(ctx, CurrentUserID(ctx))
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	defs, err := h.svc.List(ctx, bindings)
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
	bindings, err := h.bindings(ctx, CurrentUserID(ctx))
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	defs, err := h.svc.List(ctx, bindings)
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	for _, d := range defs {
		if d.Name == name {
			c.JSON(consts.StatusOK, dto.FromTool(d))
			return
		}
	}
	c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "tool not found"})
}

func (h *ToolHandler) bindings(ctx context.Context, userID int64) ([]entity.ToolBinding, error) {
	if h.plugs == nil || userID == 0 {
		return nil, nil
	}
	return h.plugs.BindingsForUser(ctx, userID)
}
