package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/server/dto"
)

type AdminHandler struct {
	svc        *admin.Service
	maxTokens  int64
	maxCostUSD float64
}

// NewAdminHandler builds the admin handler. Optional limit arguments configure
// the per-user budget shown by /me/usage (0 = unlimited).
func NewAdminHandler(svc *admin.Service, limits ...admin.UsageLimits) *AdminHandler {
	h := &AdminHandler{svc: svc}
	if len(limits) > 0 {
		h.maxTokens = limits[0].MaxTokens
		h.maxCostUSD = limits[0].MaxCostUSD
	}
	return h
}

// MeUsage returns the authenticated caller's own usage and budget (P2 D9).
func (h *AdminHandler) MeUsage(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "usage requires an authenticated user"})
		return
	}
	row, err := h.svc.UserUsage(ctx, userID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	tokensExceeded := h.maxTokens > 0 && row.Tokens >= h.maxTokens
	costExceeded := h.maxCostUSD > 0 && row.CostUSD >= h.maxCostUSD
	c.JSON(consts.StatusOK, dto.MeUsageResponse{
		UserID: row.UserID, Cases: row.Cases, Runs: row.Runs,
		Tokens: row.Tokens, CostUSD: row.CostUSD,
		MaxTokens: h.maxTokens, MaxCostUSD: h.maxCostUSD,
		TokensExceeded: tokensExceeded, CostExceeded: costExceeded,
	})
}

func (h *AdminHandler) Usage(ctx context.Context, c *app.RequestContext) {
	sum, err := h.svc.Usage(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromAdminUsage(sum))
}
