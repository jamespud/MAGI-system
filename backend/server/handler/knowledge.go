package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/knowledge"
	"github.com/jamespud/magi/backend/server/dto"
)

// KnowledgeHandler serves user knowledge documents over the HTTP API.
type KnowledgeHandler struct {
	svc *knowledge.Service
}

func NewKnowledgeHandler(svc *knowledge.Service) *KnowledgeHandler {
	return &KnowledgeHandler{svc: svc}
}

func (h *KnowledgeHandler) Create(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateKnowledgeRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "title and content are required"})
		return
	}
	doc, err := h.svc.Create(ctx, CurrentUserID(ctx), req.Title, req.Content, req.SourceKind, req.SourceURL)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromKnowledgeDoc(doc))
}

func (h *KnowledgeHandler) List(ctx context.Context, c *app.RequestContext) {
	limit, offset := 50, 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	docs, err := h.svc.List(ctx, CurrentUserID(ctx), limit, offset)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.KnowledgeDocDTO, 0, len(docs))
	for _, d := range docs {
		out = append(out, dto.FromKnowledgeDoc(d))
	}
	c.JSON(consts.StatusOK, dto.KnowledgeListResponse{Documents: out, Total: len(out)})
}

func (h *KnowledgeHandler) Get(ctx context.Context, c *app.RequestContext) {
	doc, err := h.svc.Get(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil {
		if err == knowledge.ErrForbidden {
			Forbidden(c)
			return
		}
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "knowledge doc not found"})
		return
	}
	c.JSON(consts.StatusOK, dto.FromKnowledgeDoc(doc))
}

func (h *KnowledgeHandler) Delete(ctx context.Context, c *app.RequestContext) {
	err := h.svc.Delete(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil {
		if err == knowledge.ErrForbidden {
			Forbidden(c)
			return
		}
		if err == knowledge.ErrNotFound {
			c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "knowledge doc not found"})
			return
		}
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(consts.StatusNoContent)
}
