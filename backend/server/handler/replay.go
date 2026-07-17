package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/server/dto"
)

type ReplayHandler struct {
	svc *replay.Service
}

func NewReplayHandler(svc *replay.Service) *ReplayHandler {
	return &ReplayHandler{svc: svc}
}

func (h *ReplayHandler) Events(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	events, err := h.svc.Replay(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ReplayEvent, 0, len(events))
	for _, e := range events {
		out = append(out, dto.FromEvent(e))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *ReplayHandler) Timeline(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	events, err := h.svc.Timeline(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ReplayEvent, 0, len(events))
	for _, e := range events {
		out = append(out, dto.FromEvent(e))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *ReplayHandler) Trace(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	events, err := h.svc.Trace(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ReplayEvent, 0, len(events))
	for _, e := range events {
		out = append(out, dto.FromEvent(e))
	}
	c.JSON(consts.StatusOK, out)
}
