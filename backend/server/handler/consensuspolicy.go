package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/consensuspolicy"
	"github.com/jamespud/magi/backend/server/dto"
)

type ConsensusPolicyHandler struct {
	svc *consensuspolicy.Service
}

func NewConsensusPolicyHandler(svc *consensuspolicy.Service) *ConsensusPolicyHandler {
	return &ConsensusPolicyHandler{svc: svc}
}

func (h *ConsensusPolicyHandler) Get(ctx context.Context, c *app.RequestContext) {
	policy, err := h.svc.Get(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromConsensusPolicy(policy))
}

func (h *ConsensusPolicyHandler) Update(ctx context.Context, c *app.RequestContext) {
	var req dto.ConsensusPolicyDTO
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	policy, err := h.svc.Save(ctx, req.ToEntity())
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromConsensusPolicy(policy))
}

func (h *ConsensusPolicyHandler) Reset(ctx context.Context, c *app.RequestContext) {
	policy, err := h.svc.Reset(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromConsensusPolicy(policy))
}
