package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/selfimprove"
	"github.com/jamespud/magi/backend/server/dto"
)

type SelfImproveHandler struct {
	svc *selfimprove.Service
}

func NewSelfImproveHandler(svc *selfimprove.Service) *SelfImproveHandler {
	return &SelfImproveHandler{svc: svc}
}

func (h *SelfImproveHandler) Analyze(ctx context.Context, c *app.RequestContext) {
	var req dto.AnalyzeSelfImproveRequest
	if err := c.BindAndValidate(&req); err != nil || req.CaseID == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "case_id is required"})
		return
	}
	suggestion, err := h.svc.Analyze(ctx, req.CaseID)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromSelfImprove(suggestion))
}

func (h *SelfImproveHandler) List(ctx context.Context, c *app.RequestContext) {
	suggestions, err := h.svc.List(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.SelfImproveSuggestionResponse, 0, len(suggestions))
	for _, s := range suggestions {
		out = append(out, dto.FromSelfImprove(s))
	}
	c.JSON(consts.StatusOK, map[string]any{"suggestions": out})
}

func (h *SelfImproveHandler) Apply(ctx context.Context, c *app.RequestContext) {
	suggestion, err := h.svc.Apply(ctx, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromSelfImprove(suggestion))
}
