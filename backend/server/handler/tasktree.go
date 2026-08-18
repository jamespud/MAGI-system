package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/dto"
)

type TaskTreeHandler struct {
	repo    port.TaskTreeRepository
	caseSvc CaseGetter
}

func NewTaskTreeHandler(repo port.TaskTreeRepository, caseSvc CaseGetter) *TaskTreeHandler {
	return &TaskTreeHandler{repo: repo, caseSvc: caseSvc}
}

func (h *TaskTreeHandler) List(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if h.caseSvc != nil {
		case_, _ := h.caseSvc.Get(ctx, id)
		if !CaseAllowed(ctx, case_) {
			Forbidden(c)
			return
		}
	}
	if h.repo == nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: "task tree repository not configured"})
		return
	}
	nodes, err := h.repo.ListByCase(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.TaskNodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, dto.FromTaskNode(n))
	}
	c.JSON(consts.StatusOK, map[string]any{"nodes": out})
}
