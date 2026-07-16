package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/server/dto"
)

type EvaluationHandler struct {
	svc *evaluation.Service
}

func NewEvaluationHandler(svc *evaluation.Service) *EvaluationHandler {
	return &EvaluationHandler{svc: svc}
}

func (h *EvaluationHandler) Evaluate(ctx context.Context, c *app.RequestContext) {
	ev, err := h.svc.EvaluateCase(ctx, "")
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromEvaluation(ev))
}
