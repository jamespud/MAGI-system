package entity

import "time"

// Conversation is a persistent multi-turn thread that links assistant
// questions to their decision cases so users can follow up on prior decisions
// with full conversational context (AI Harness: session & multi-turn).
type Conversation struct {
	ID        string
	UserID    int64
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ConversationMessage is one turn in a conversation. User turns carry the
// question; assistant turns link to the decision case created for that turn.
type ConversationMessage struct {
	ID             string
	ConversationID string
	UserID         int64
	Role           string // ConversationRoleUser | ConversationRoleAssistant
	Content        string
	CaseID         string // linked decision case (assistant turns)
	CreatedAt      time.Time
}

const (
	ConversationRoleUser      = "user"
	ConversationRoleAssistant = "assistant"
)
