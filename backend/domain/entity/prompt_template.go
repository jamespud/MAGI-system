package entity

import "time"

// PromptTemplate is a versioned LLM prompt template (P2 D12). Runtime prompt
// composition (Commander normalize/report, agent workflow blocks) loads the
// active version by key and falls back to built-in defaults when the store is
// empty. Content uses {{PLACEHOLDER}} tokens substituted by the caller.
type PromptTemplate struct {
	Key       string
	Version   int
	Content   string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
