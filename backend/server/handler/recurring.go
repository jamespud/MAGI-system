package handler

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type RecurringHandler struct {
	svc *recurring.Service
}

func NewRecurringHandler(svc *recurring.Service) *RecurringHandler {
	return &RecurringHandler{svc: svc}
}

func (h *RecurringHandler) List(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	items, err := h.svc.List(ctx, userID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.RecurringResponse, 0, len(items))
	for _, r := range items {
		out = append(out, dto.FromRecurring(r))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *RecurringHandler) Create(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	var req dto.CreateRecurringRequest
	if err := c.BindAndValidate(&req); err != nil || req.IntervalSeconds <= 0 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "name, question and interval_seconds are required"})
		return
	}
	constraints := make([]entity.Constraint, len(req.Constraints))
	for i, ct := range req.Constraints {
		constraints[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
	}
	r, err := h.svc.Create(ctx, userID, req.Name, req.Question, req.Background, constraints, time.Duration(req.IntervalSeconds)*time.Second)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromRecurring(r))
}

func (h *RecurringHandler) Get(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	r, err := h.svc.Get(ctx, userID, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromRecurring(r))
}

func (h *RecurringHandler) SetEnabled(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	var req dto.SetRecurringEnabledRequest
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

func (h *RecurringHandler) Delete(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	if err := h.svc.Delete(ctx, userID, c.Param("id")); err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusNoContent, nil)
}

func (h *RecurringHandler) RunNow(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		Forbidden(c)
		return
	}
	case_, err := h.svc.RunNow(ctx, userID, c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusAccepted, dto.CaseResponse{ID: case_.ID, Status: string(case_.Status)})
}
