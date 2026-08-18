package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/investigationplan"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type InvestigationPlanHandler struct {
	svc     *investigationplan.Service
	caseSvc CaseGetter
}

func NewInvestigationPlanHandler(svc *investigationplan.Service, caseSvc CaseGetter) *InvestigationPlanHandler {
	return &InvestigationPlanHandler{svc: svc, caseSvc: caseSvc}
}

func (h *InvestigationPlanHandler) authorize(ctx context.Context, c *app.RequestContext, id string) bool {
	if h.caseSvc == nil {
		return true
	}
	case_, _ := h.caseSvc.Get(ctx, id)
	if CaseAllowed(ctx, case_) {
		return true
	}
	Forbidden(c)
	return false
}

func (h *InvestigationPlanHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if !h.authorize(ctx, c, id) {
		return
	}
	plan, err := h.svc.Get(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if plan == nil {
		c.JSON(consts.StatusOK, dto.FromInvestigationPlan(nil))
		return
	}
	c.JSON(consts.StatusOK, dto.FromInvestigationPlan(plan))
}

func (h *InvestigationPlanHandler) Update(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if !h.authorize(ctx, c, id) {
		return
	}
	var req struct {
		Items []entity.InvestigationPlanItem `json:"items"`
	}
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	plan, err := h.svc.Save(ctx, id, req.Items)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromInvestigationPlan(plan))
}
