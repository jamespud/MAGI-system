package handler

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type ApprovalHandler struct {
	svc     *approval.Service
	caseSvc CaseGetter
}

func NewApprovalHandler(svc *approval.Service, caseSvc CaseGetter) *ApprovalHandler {
	return &ApprovalHandler{svc: svc, caseSvc: caseSvc}
}

func (h *ApprovalHandler) canAccess(ctx context.Context, caseID string) bool {
	if h.caseSvc == nil {
		return true
	}
	cs, err := h.caseSvc.Get(ctx, caseID)
	if err != nil {
		return false
	}
	return CaseAllowed(ctx, cs)
}

func (h *ApprovalHandler) List(ctx context.Context, c *app.RequestContext) {
	caseID := c.Query("case_id")
	reqs, err := h.svc.List(ctx, caseID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ApprovalRequestDTO, 0, len(reqs))
	for _, r := range reqs {
		if caseID != "" || h.canAccess(ctx, r.CaseID) {
			out = append(out, dto.FromApproval(r))
		}
	}
	c.JSON(consts.StatusOK, dto.ApprovalListResponse{Approvals: out})
}

func (h *ApprovalHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	req, err := h.svc.Get(ctx, id)
	if err != nil || req == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "approval not found"})
		return
	}
	if !h.canAccess(ctx, req.CaseID) {
		Forbidden(c)
		return
	}
	c.JSON(consts.StatusOK, dto.FromApproval(req))
}

func (h *ApprovalHandler) Approve(ctx context.Context, c *app.RequestContext) {
	h.decide(ctx, c, true)
}

func (h *ApprovalHandler) Reject(ctx context.Context, c *app.RequestContext) {
	h.decide(ctx, c, false)
}

func (h *ApprovalHandler) decide(ctx context.Context, c *app.RequestContext, approve bool) {
	id := c.Param("id")
	req, err := h.svc.Get(ctx, id)
	if err != nil || req == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "approval not found"})
		return
	}
	if !h.canAccess(ctx, req.CaseID) {
		Forbidden(c)
		return
	}
	var body dto.ApprovalDecisionRequest
	_ = c.BindAndValidate(&body)
	decidedBy := ""
	if p := auth.PrincipalFrom(ctx); p != nil {
		decidedBy = p.Name
		if decidedBy == "" {
			decidedBy = fmt.Sprintf("user-%d", p.UserID)
		}
	}
	var updated *entity.ApprovalRequest
	if approve {
		updated, err = h.svc.Approve(ctx, id, decidedBy, body.Reason)
	} else {
		updated, err = h.svc.Reject(ctx, id, decidedBy, body.Reason)
	}
	if err != nil {
		c.JSON(consts.StatusConflict, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromApproval(updated))
}
