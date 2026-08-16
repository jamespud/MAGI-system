package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// PromptRepository persists versioned prompt templates (P2 D12).
type PromptRepository interface {
	// List returns all versions of all keys, newest first.
	List(ctx context.Context) ([]*entity.PromptTemplate, error)
	// Get returns the active version of a key, or nil when absent.
	Get(ctx context.Context, key string) (*entity.PromptTemplate, error)
	// Save upserts a new version of key: v1 on first write, otherwise it bumps
	// the version and marks the new version active (previous versions become
	// inactive).
	Save(ctx context.Context, key, content string) (*entity.PromptTemplate, error)
	// Restore forces a specific content to be the active version without
	// incrementing the counter (used for seeding and reset-to-default).
	Restore(ctx context.Context, key, content string) (*entity.PromptTemplate, error)
}

// PromptProvider is the read-only prompt source used by the runtime.
type PromptProvider interface {
	// Load returns the active content for key.
	Load(ctx context.Context, key string) (string, bool)
}
