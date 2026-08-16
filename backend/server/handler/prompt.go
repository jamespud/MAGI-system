package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/prompt"
	"github.com/jamespud/magi/backend/server/dto"
)

// PromptHandler serves the versioned prompt registry (P2 D12). All routes are
// admin-gated at the router level.
type PromptHandler struct {
	repo port.PromptRepository
}

func NewPromptHandler(repo port.PromptRepository) *PromptHandler {
	return &PromptHandler{repo: repo}
}

func (h *PromptHandler) List(ctx context.Context, c *app.RequestContext) {
	if h.repo == nil {
		c.JSON(consts.StatusServiceUnavailable, dto.ErrorResponse{Error: "prompt registry not configured"})
		return
	}
	templates, err := h.repo.List(ctx)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.PromptDTO, 0, len(templates))
	for _, t := range templates {
		out = append(out, dto.FromPrompt(t))
	}
	c.JSON(consts.StatusOK, dto.PromptListResponse{Prompts: out})
}

func (h *PromptHandler) Get(ctx context.Context, c *app.RequestContext) {
	if h.repo == nil {
		c.JSON(consts.StatusServiceUnavailable, dto.ErrorResponse{Error: "prompt registry not configured"})
		return
	}
	key := c.Param("key")
	if key == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "key is required"})
		return
	}
	t, err := h.repo.Get(ctx, key)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if t == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "prompt key not found: " + key})
		return
	}
	c.JSON(consts.StatusOK, dto.FromPrompt(t))
}

func (h *PromptHandler) Update(ctx context.Context, c *app.RequestContext) {
	if h.repo == nil {
		c.JSON(consts.StatusServiceUnavailable, dto.ErrorResponse{Error: "prompt registry not configured"})
		return
	}
	key := c.Param("key")
	var req dto.UpdatePromptRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "content is required"})
		return
	}
	t, err := h.repo.Save(ctx, key, req.Content)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromPrompt(t))
}

func (h *PromptHandler) Restore(ctx context.Context, c *app.RequestContext) {
	if h.repo == nil {
		c.JSON(consts.StatusServiceUnavailable, dto.ErrorResponse{Error: "prompt registry not configured"})
		return
	}
	key := c.Param("key")
	content, ok := prompt.Default()[key]
	if !ok {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "no built-in default for key: " + key})
		return
	}
	t, err := h.repo.Restore(ctx, key, content)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromPrompt(t))
}
