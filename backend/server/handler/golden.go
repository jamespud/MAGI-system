package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/golden"
	"github.com/jamespud/magi/backend/server/dto"
)

type GoldenHandler struct {
	svc *golden.Service
}

func NewGoldenHandler(svc *golden.Service) *GoldenHandler {
	return &GoldenHandler{svc: svc}
}

func (h *GoldenHandler) Add(ctx context.Context, c *app.RequestContext) {
	var req dto.AddGoldenRequest
	if err := c.BindAndValidate(&req); err != nil || req.CaseID == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "case_id is required"})
		return
	}
	g, err := h.svc.Add(ctx, req.CaseID)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromGoldenCase(g))
}

func (h *GoldenHandler) List(ctx context.Context, c *app.RequestContext) {
	list, err := h.svc.List(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.GoldenCaseResponse, 0, len(list))
	for _, g := range list {
		out = append(out, dto.FromGoldenCase(g))
	}
	c.JSON(consts.StatusOK, map[string]any{"golden": out})
}

func (h *GoldenHandler) Delete(ctx context.Context, c *app.RequestContext) {
	if err := h.svc.Delete(ctx, c.Param("id")); err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *GoldenHandler) Sync(ctx context.Context, c *app.RequestContext) {
	count, err := h.svc.SyncToDataset(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"synced": count})
}
