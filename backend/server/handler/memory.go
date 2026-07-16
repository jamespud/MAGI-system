package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/server/dto"
)

type MemoryHandler struct {
	svc *memory.Service
}

func NewMemoryHandler(svc *memory.Service) *MemoryHandler {
	return &MemoryHandler{svc: svc}
}

func (h *MemoryHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	proj, err := h.svc.Get(ctx, id)
	if err != nil || proj == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "memory not found"})
		return
	}
	c.JSON(consts.StatusOK, proj)
}
