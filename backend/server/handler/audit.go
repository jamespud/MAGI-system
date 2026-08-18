package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/server/dto"
)

// AuditHandler serves the administrative/security audit trail.
type AuditHandler struct {
	svc *audit.Service
}

func NewAuditHandler(svc *audit.Service) *AuditHandler {
	return &AuditHandler{svc: svc}
}

func (h *AuditHandler) List(ctx context.Context, c *app.RequestContext) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	events, total, err := h.svc.List(ctx, limit, offset)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.AuditEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, dto.FromAuditEvent(e))
	}
	c.JSON(consts.StatusOK, map[string]any{"events": out, "total": total})
}
