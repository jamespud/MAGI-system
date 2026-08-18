package handler

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/server/dto"
)

type ConversationHandler struct {
	svc *assistant.Service
}

func NewConversationHandler(svc *assistant.Service) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// List returns the caller's conversation threads, most recently active first.
func (h *ConversationHandler) List(ctx context.Context, c *app.RequestContext) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	convs, err := h.svc.ListConversations(ctx, CurrentUserID(ctx), limit, offset)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ConversationDTO, len(convs))
	for i, conv := range convs {
		out[i] = dto.FromConversation(conv)
	}
	c.JSON(consts.StatusOK, dto.ConversationListResponse{Conversations: out})
}

// Get returns one conversation thread plus its messages.
func (h *ConversationHandler) Get(ctx context.Context, c *app.RequestContext) {
	conv, msgs, err := h.svc.GetConversation(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil {
		status := consts.StatusInternalServerError
		if errors.Is(err, assistant.ErrForbidden) {
			status = consts.StatusForbidden
		} else if errors.Is(err, assistant.ErrConversationNotFound) {
			status = consts.StatusNotFound
		}
		c.JSON(status, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ConversationMessageDTO, len(msgs))
	for i, m := range msgs {
		out[i] = dto.FromConversationMessage(m)
	}
	c.JSON(consts.StatusOK, dto.ConversationDetailResponse{
		Conversation: dto.FromConversation(conv),
		Messages:     out,
	})
}

// Delete removes a conversation thread and its messages. The linked decision
// cases are preserved as audit records.
func (h *ConversationHandler) Delete(ctx context.Context, c *app.RequestContext) {
	if err := h.svc.DeleteConversation(ctx, CurrentUserID(ctx), c.Param("id")); err != nil {
		status := consts.StatusInternalServerError
		if errors.Is(err, assistant.ErrForbidden) {
			status = consts.StatusForbidden
		} else if errors.Is(err, assistant.ErrConversationNotFound) {
			status = consts.StatusNotFound
		}
		c.JSON(status, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusNoContent, nil)
}
