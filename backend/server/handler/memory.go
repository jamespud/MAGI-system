package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/server/dto"
)

type MemoryHandler struct {
	svc     *memory.Service
	caseSvc CaseGetter
}

func NewMemoryHandler(svc *memory.Service, caseSvc CaseGetter) *MemoryHandler {
	return &MemoryHandler{svc: svc, caseSvc: caseSvc}
}

func (h *MemoryHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if h.caseSvc != nil {
		case_, _ := h.caseSvc.Get(ctx, id)
		if !CaseAllowed(ctx, case_) {
			Forbidden(c)
			return
		}
	}
	proj, err := h.svc.Get(ctx, id)
	if err != nil || proj == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "memory not found"})
		return
	}
	c.JSON(consts.StatusOK, proj)
}

// Search returns historical decision memories the caller owns.
func (h *MemoryHandler) Search(ctx context.Context, c *app.RequestContext) {
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	projs, err := h.svc.Search(ctx, CurrentUserID(ctx), c.Query("q"), limit)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.MemorySearchResponse{Results: projs})
}
