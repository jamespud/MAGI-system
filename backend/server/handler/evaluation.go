package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/server/dto"
)

type EvaluationHandler struct {
	svc     *evaluation.Service
	caseSvc CaseGetter
}

type evaluateRequest struct {
	CaseID string `json:"case_id"`
}

type benchmarkRequest struct {
	CaseIDs []string `json:"case_ids"`
}

func NewEvaluationHandler(svc *evaluation.Service, caseSvc CaseGetter) *EvaluationHandler {
	return &EvaluationHandler{svc: svc, caseSvc: caseSvc}
}

func (h *EvaluationHandler) authorize(ctx context.Context, c *app.RequestContext, caseID string) bool {
	if h.caseSvc == nil {
		return true
	}
	case_, _ := h.caseSvc.Get(ctx, caseID)
	if CaseAllowed(ctx, case_) {
		return true
	}
	Forbidden(c)
	return false
}

func (h *EvaluationHandler) Evaluate(ctx context.Context, c *app.RequestContext) {
	caseID := c.Param("id")
	if caseID == "" {
		caseID = c.Query("case_id")
	}
	if caseID == "" {
		var req evaluateRequest
		if c.BindAndValidate(&req) == nil {
			caseID = req.CaseID
		}
	}
	if caseID == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "case_id is required"})
		return
	}
	if !h.authorize(ctx, c, caseID) {
		return
	}
	ev, err := h.svc.EvaluateCase(ctx, caseID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromEvaluation(ev))
}

func (h *EvaluationHandler) Benchmark(ctx context.Context, c *app.RequestContext) {
	var req benchmarkRequest
	if err := c.BindAndValidate(&req); err != nil || len(req.CaseIDs) == 0 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "case_ids is required"})
		return
	}
	for _, id := range req.CaseIDs {
		if !h.authorize(ctx, c, id) {
			return
		}
	}
	evals, err := h.svc.Benchmark(ctx, req.CaseIDs)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make(map[string]dto.EvaluationResponse, len(evals))
	for caseID, ev := range evals {
		if ev != nil {
			out[caseID] = dto.FromEvaluation(ev)
		}
	}
	c.JSON(consts.StatusOK, out)
}
