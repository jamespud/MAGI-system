package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/fsmblueprint"
	"github.com/jamespud/magi/backend/server/dto"
)

type FSMBlueprintHandler struct {
	svc *fsmblueprint.Service
}

func NewFSMBlueprintHandler(svc *fsmblueprint.Service) *FSMBlueprintHandler {
	return &FSMBlueprintHandler{svc: svc}
}

func (h *FSMBlueprintHandler) Get(ctx context.Context, c *app.RequestContext) {
	blueprint, err := h.svc.Get(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromFSMBlueprint(blueprint))
}

func (h *FSMBlueprintHandler) Update(ctx context.Context, c *app.RequestContext) {
	var req dto.FSMBlueprintDTO
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	blueprint, err := h.svc.Save(ctx, req.ToEntity())
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromFSMBlueprint(blueprint))
}

func (h *FSMBlueprintHandler) Reset(ctx context.Context, c *app.RequestContext) {
	blueprint, err := h.svc.Reset(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromFSMBlueprint(blueprint))
}

func (h *FSMBlueprintHandler) Validate(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Path []string `json:"path"`
	}
	if err := c.BindAndValidate(&req); err != nil || len(req.Path) < 2 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "path must contain at least two statuses"})
		return
	}
	violations, err := h.svc.ValidatePath(ctx, req.Path)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": len(violations) == 0, "violations": violations})
}
