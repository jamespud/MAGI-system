package handler

import (
	"context"
	"errors"

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
// run. It returns 202 + case ID; the run executes through the governed async
// runner (concurrency limits, budgets, leases, retries).
func (h *AssistantHandler) Ask(ctx context.Context, c *app.RequestContext) {
	var req dto.AskRequest
	if err := c.BindAndValidate(&req); err != nil || req.Message == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "message is required"})
		return
	}
	cs, conv, err := h.svc.AskInConversation(ctx, CurrentUserID(ctx), req.ConversationID, req.Message, req.Background, toConstraints(req.Constraints))
	if err != nil {
		status := consts.StatusBadRequest
		if errors.Is(err, assistant.ErrForbidden) {
			status = consts.StatusForbidden
		} else if errors.Is(err, assistant.ErrConversationNotFound) {
			status = consts.StatusNotFound
		}
		c.JSON(status, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if conv != nil {
		c.JSON(consts.StatusAccepted, dto.AskInConversationResponse{
			CaseResponse:   dto.FromCase(cs, nil),
			ConversationID: conv.ID,
		})
		return
	}
	c.JSON(consts.StatusAccepted, dto.CaseResponse{ID: cs.ID, Status: string(cs.Status)})
}

func toConstraints(dtos []dto.ConstraintDTO) []entity.Constraint {
	out := make([]entity.Constraint, len(dtos))
	for i, ct := range dtos {
		out[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
	}
	return out
}
