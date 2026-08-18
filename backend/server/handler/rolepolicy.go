package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/rolepolicy"
	"github.com/jamespud/magi/backend/server/dto"
)

type RolePolicyHandler struct {
	svc *rolepolicy.Service
}

func NewRolePolicyHandler(svc *rolepolicy.Service) *RolePolicyHandler {
	return &RolePolicyHandler{svc: svc}
}

func (h *RolePolicyHandler) List(ctx context.Context, c *app.RequestContext) {
	policies, err := h.svc.List(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.RolePolicyDTO, 0, len(policies))
	for _, p := range policies {
		out = append(out, dto.FromRolePolicy(p))
	}
	c.JSON(consts.StatusOK, map[string]any{"policies": out})
}

func (h *RolePolicyHandler) Update(ctx context.Context, c *app.RequestContext) {
	var req dto.RolePolicyDTO
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	policy, err := h.svc.Save(ctx, c.Param("code"), req.ToEntity())
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromRolePolicy(policy))
}

func (h *RolePolicyHandler) Reset(ctx context.Context, c *app.RequestContext) {
	policy, err := h.svc.Reset(ctx, c.Param("code"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromRolePolicy(policy))
}
