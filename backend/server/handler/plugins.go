package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/server/dto"
)

type PluginsHandler struct {
	svc *plugins.Service
}

type createPluginRequest struct {
	PluginID int64 `json:"plugin_id"`
	ToolID   int64 `json:"tool_id"`
	IsDraft  bool  `json:"is_draft,omitempty"`
	Enabled  *bool `json:"enabled,omitempty"`
}

type updatePluginRequest struct {
	Enabled bool `json:"enabled"`
}

func NewPluginsHandler(svc *plugins.Service) *PluginsHandler {
	return &PluginsHandler{svc: svc}
}

func (h *PluginsHandler) List(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
		return
	}
	bindings, err := h.svc.List(ctx, userID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.PluginBindingResponse, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, dto.FromPluginBinding(b))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *PluginsHandler) Create(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
		return
	}
	var req createPluginRequest
	if err := c.BindAndValidate(&req); err != nil || req.PluginID <= 0 || req.ToolID <= 0 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "plugin_id and tool_id are required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	b, err := h.svc.Create(ctx, userID, req.PluginID, req.ToolID, req.IsDraft, enabled)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromPluginBinding(b))
}

func (h *PluginsHandler) SetEnabled(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
		return
	}
	var req updatePluginRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "enabled is required"})
		return
	}
	if err := h.svc.SetEnabled(ctx, userID, c.Param("id"), req.Enabled); err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.ErrorResponse{Error: ""})
}

func (h *PluginsHandler) Delete(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
		return
	}
	if err := h.svc.Delete(ctx, userID, c.Param("id")); err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusNoContent, nil)
}
