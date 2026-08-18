// Package assistant is the conversational entry point: natural-language
// decision questions become full MAGI decision runs, optionally inside a
// persistent multi-turn conversation thread.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ErrForbidden is returned when a principal tries to access a conversation
// they do not own.
var ErrForbidden = errors.New("forbidden")

// ErrConversationNotFound is returned when a conversation does not exist.
var ErrConversationNotFound = errors.New("conversation not found")

// Service is the conversational entry point: a natural-language decision
// question becomes a full MAGI decision run, and the caller receives the
// final decision with its report.
type Service struct {
	dec      *decision.Service
	convRepo port.ConversationRepository
}

// Option configures a Service.
type Option func(*Service)

// WithConversationRepository enables persistent multi-turn conversations.
func WithConversationRepository(repo port.ConversationRepository) Option {
	return func(s *Service) { s.convRepo = repo }
}

// NewService creates an assistant Service. Without a conversation repository
// it behaves as the legacy single-shot assistant.
func NewService(dec *decision.Service, opts ...Option) *Service {
	s := &Service{dec: dec}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// AskAsync creates a case and hands it to the governed async runner
// (concurrency limits, budgets, leases, retries all apply). The caller polls
// the returned case ID for the final resolution.
func (s *Service) AskAsync(ctx context.Context, userID int64, message, background string, constraints []entity.Constraint) (*entity.DecisionCase, error) {
	case_, _, err := s.AskInConversation(ctx, userID, "", message, background, constraints)
	return case_, err
}

// AskInConversation asks a question inside a conversation thread. An empty
// conversationID starts a new thread. A follow-up question automatically
// hydrates prior turns and their decision outcomes into the new case's
// background so the agents see the full conversational context.
func (s *Service) AskInConversation(ctx context.Context, userID int64, conversationID, message, background string, constraints []entity.Constraint) (*entity.DecisionCase, *entity.Conversation, error) {
	if message == "" {
		return nil, nil, fmt.Errorf("assistant: message is required")
	}

	var conv *entity.Conversation
	if conversationID == "" {
		conv = &entity.Conversation{
			ID:     fmt.Sprintf("conv-%s", uuid.NewString()),
			UserID: userID,
			Title:  conversationTitle(message),
		}
		if s.convRepo != nil {
			if err := s.convRepo.Create(ctx, conv); err != nil {
				return nil, nil, fmt.Errorf("assistant: create conversation: %w", err)
			}
		}
	} else {
		if s.convRepo == nil {
			return nil, nil, ErrConversationNotFound
		}
		var err error
		conv, err = s.convRepo.Get(ctx, conversationID)
		if err != nil {
			return nil, nil, ErrConversationNotFound
		}
		if conv.UserID != 0 && conv.UserID != userID {
			return nil, nil, ErrForbidden
		}
		background, err = s.followUpBackground(ctx, conv, background)
		if err != nil {
			return nil, nil, err
		}
	}

	if s.convRepo != nil {
		if err := s.convRepo.AppendMessage(ctx, &entity.ConversationMessage{
			ID:             fmt.Sprintf("msg-%s", uuid.NewString()),
			ConversationID: conv.ID,
			UserID:         userID,
			Role:           entity.ConversationRoleUser,
			Content:        message,
		}); err != nil {
			return nil, nil, fmt.Errorf("assistant: append user message: %w", err)
		}
	}

	case_, err := s.dec.Create(ctx, userID, message, background, constraints)
	if err != nil {
		return nil, conv, fmt.Errorf("assistant: create case: %w", err)
	}
	if err := s.dec.StartRun(ctx, case_); err != nil {
		return case_, conv, fmt.Errorf("assistant: start run: %w", err)
	}

	if s.convRepo != nil {
		if err := s.convRepo.AppendMessage(ctx, &entity.ConversationMessage{
			ID:             fmt.Sprintf("msg-%s", uuid.NewString()),
			ConversationID: conv.ID,
			UserID:         userID,
			Role:           entity.ConversationRoleAssistant,
			Content:        fmt.Sprintf("Decision case %s created and started.", case_.ID),
			CaseID:         case_.ID,
		}); err != nil {
			return case_, conv, fmt.Errorf("assistant: append assistant message: %w", err)
		}
	}
	return case_, conv, nil
}

// followUpBackground folds the conversation history (user turns plus linked
// decision outcomes) into the background for a follow-up case.
func (s *Service) followUpBackground(ctx context.Context, conv *entity.Conversation, extra string) (string, error) {
	// Request the bounded maximum, then keep only the most recent turns. This
	// keeps repositories free to return messages in chronological order while
	// still hydrating the latest context for long-running threads.
	msgs, err := s.convRepo.ListMessages(ctx, conv.ID, 500)
	if err != nil {
		return "", fmt.Errorf("assistant: load conversation history: %w", err)
	}
	if len(msgs) > 20 {
		msgs = msgs[len(msgs)-20:]
	}
	var b strings.Builder
	wrote := false
	for _, m := range msgs {
		switch m.Role {
		case entity.ConversationRoleUser:
			b.WriteString("User: " + m.Content + "\n")
			wrote = true
		case entity.ConversationRoleAssistant:
			if m.CaseID == "" {
				continue
			}
			if res, _ := s.dec.Resolution(ctx, m.CaseID); res != nil {
				fmt.Fprintf(&b, "Previous decision (%s): %s (consensus=%s, round=%d)\n",
					m.CaseID, res.FinalDecision, res.Consensus.Outcome, res.Consensus.Round)
				wrote = true
			}
		}
	}
	if !wrote {
		return extra, nil
	}
	var out strings.Builder
	out.WriteString("[Conversation history]\n")
	out.WriteString(b.String())
	if strings.TrimSpace(extra) != "" {
		out.WriteString("\n[Additional background]\n")
		out.WriteString(extra)
	}
	return out.String(), nil
}

// conversationTitle derives a thread title from the first user message.
func conversationTitle(message string) string {
	t := strings.TrimSpace(message)
	if runes := []rune(t); len(runes) > 80 {
		t = string(runes[:80])
	}
	if t == "" {
		t = "New conversation"
	}
	return t
}

// ListConversations returns the caller's threads, most recently active first.
func (s *Service) ListConversations(ctx context.Context, userID int64, limit, offset int) ([]*entity.Conversation, error) {
	if s.convRepo == nil {
		return nil, nil
	}
	return s.convRepo.ListByUser(ctx, userID, limit, offset)
}

// GetConversation returns one thread plus its messages.
func (s *Service) GetConversation(ctx context.Context, userID int64, id string) (*entity.Conversation, []*entity.ConversationMessage, error) {
	if s.convRepo == nil {
		return nil, nil, ErrConversationNotFound
	}
	conv, err := s.convRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, ErrConversationNotFound
	}
	if conv.UserID != 0 && conv.UserID != userID {
		return nil, nil, ErrForbidden
	}
	msgs, err := s.convRepo.ListMessages(ctx, id, 500)
	if err != nil {
		return nil, nil, err
	}
	return conv, msgs, nil
}

// DeleteConversation removes a thread and its messages (decision cases are
// preserved as first-class audit records).
func (s *Service) DeleteConversation(ctx context.Context, userID int64, id string) error {
	if s.convRepo == nil {
		return ErrConversationNotFound
	}
	conv, err := s.convRepo.Get(ctx, id)
	if err != nil {
		return ErrConversationNotFound
	}
	if conv.UserID != 0 && conv.UserID != userID {
		return ErrForbidden
	}
	return s.convRepo.Delete(ctx, id)
}
