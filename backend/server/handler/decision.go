package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/server/dto"
)

type DecisionHandler struct {
	svc *decision.Service
}

func NewDecisionHandler(svc *decision.Service) *DecisionHandler {
	return &DecisionHandler{svc: svc}
}

func (h *DecisionHandler) Create(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateCaseRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	case_, err := h.svc.Create(ctx, req.Question)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromCase(case_))
}

func (h *DecisionHandler) Run(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	res, err := h.svc.Run(ctx, case_)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromResolution(res))
}

func (h *DecisionHandler) Cancel(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if err := h.svc.Cancel(ctx, id); err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.CaseResponse{ID: id, Status: "CANCELLED"})
}

func (h *DecisionHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	c.JSON(consts.StatusOK, dto.FromCase(case_))
}

func (h *DecisionHandler) Report(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	report := h.svc.Report(ctx, case_, nil)
	c.JSON(consts.StatusOK, dto.DecisionReport{Report: report})
}

func (h *DecisionHandler) List(ctx context.Context, c *app.RequestContext) {
	cases, err := h.svc.List(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.CaseResponse, 0, len(cases))
	for _, cs := range cases {
		out = append(out, dto.FromCase(cs))
	}
	c.JSON(consts.StatusOK, out)
}
