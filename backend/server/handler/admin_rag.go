package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/ragindex"
	"github.com/jamespud/magi/backend/server/dto"
)

// AdminRagHandler exposes admin RAG operations (reindex).
type AdminRagHandler struct {
	svc *ragindex.Service
}

func NewAdminRagHandler(svc *ragindex.Service) *AdminRagHandler {
	return &AdminRagHandler{svc: svc}
}

// Reindex enqueues index jobs for the requested source.
// POST /api/v1/admin/rag/reindex?source=case_memory|knowledge_doc|all
func (h *AdminRagHandler) Reindex(ctx context.Context, c *app.RequestContext) {
	source := c.Query("source")
	n, err := h.svc.Reindex(ctx, source)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"enqueued": n, "source": source})
}
