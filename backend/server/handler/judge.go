package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/server/dto"
)

type JudgeHandler struct {
	svc     *judge.Service
	caseSvc CaseGetter
}

func NewJudgeHandler(svc *judge.Service, caseSvc CaseGetter) *JudgeHandler {
	return &JudgeHandler{svc: svc, caseSvc: caseSvc}
}

func (h *JudgeHandler) Judge(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	cs, err := h.caseSvc.Get(ctx, id)
	if err != nil || cs == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !CaseAllowed(ctx, cs) {
		Forbidden(c)
		return
	}
	res, err := h.svc.Judge(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromJudge(res))
}
