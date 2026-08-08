package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type AssistantHandler struct {
	svc *assistant.Service
}

func NewAssistantHandler(svc *assistant.Service) *AssistantHandler {
	return &AssistantHandler{svc: svc}
}

// Ask turns a natural-language decision question into a full MAGI decision
// run and returns the final decision with its report.
func (h *AssistantHandler) Ask(ctx context.Context, c *app.RequestContext) {
	var req dto.AskRequest
	if err := c.BindAndValidate(&req); err != nil || req.Message == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "message is required"})
		return
	}
	constraints := make([]entity.Constraint, len(req.Constraints))
	for i, ct := range req.Constraints {
		constraints[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
	}
	cs, res, err := h.svc.Ask(ctx, CurrentUserID(ctx), req.Message, req.Background, constraints)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.FromAsk(cs, res))
}
