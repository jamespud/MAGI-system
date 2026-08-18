package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Memory update governance errors.
var (
	ErrMemoryNotFound = errors.New("memory not found")
	ErrForbidden      = errors.New("forbidden")
	ErrNoFields       = errors.New("no fields to update")
	ErrInvalidMemory  = errors.New("invalid memory")
)

// UpdatePatch contains user-editable long-term-memory fields. Nil pointers
// preserve the existing value; a non-nil empty slice clears tags.
type UpdatePatch struct {
	QuestionSummary *string
	ContextSummary  *string
	Resolution      *string
	Learned         *string
	Annotation      *string
	Tags            []string
}

// Update edits a user-owned memory projection and refreshes its RAG index.
// If reindexing fails after the SQL update, the previous projection is
// restored so the database and index do not knowingly diverge.
func (s *Service) Update(ctx context.Context, userID int64, caseID string, patch UpdatePatch) (*entity.CaseMemoryProjection, error) {
	if s.memRepo == nil {
		return nil, ErrMemoryNotFound
	}
	if caseID == "" {
		return nil, ErrMemoryNotFound
	}

	current, err := s.memRepo.Get(ctx, caseID)
	if err != nil || current == nil {
		return nil, ErrMemoryNotFound
	}
	if !s.allowed(ctx, userID, current.CaseID) {
		return nil, ErrForbidden
	}

	updated := cloneProjection(*current)
	changed := false
	if patch.QuestionSummary != nil {
		value := strings.TrimSpace(*patch.QuestionSummary)
		if value == "" {
			return nil, fmt.Errorf("%w: question summary cannot be empty", ErrInvalidMemory)
		}
		if len([]rune(value)) > 500 {
			return nil, fmt.Errorf("%w: question summary exceeds 500 characters", ErrInvalidMemory)
		}
		updated.QuestionSummary = value
		changed = true
	}
	if patch.ContextSummary != nil {
		value := strings.TrimSpace(*patch.ContextSummary)
		if len([]rune(value)) > 4000 {
			return nil, fmt.Errorf("%w: context summary exceeds 4000 characters", ErrInvalidMemory)
		}
		updated.ContextSummary = value
		changed = true
	}
	if patch.Resolution != nil {
		value := strings.TrimSpace(*patch.Resolution)
		if len([]rune(value)) > 4000 {
			return nil, fmt.Errorf("%w: resolution exceeds 4000 characters", ErrInvalidMemory)
		}
		updated.Resolution = value
		changed = true
	}
	if patch.Learned != nil {
		value := strings.TrimSpace(*patch.Learned)
		if len([]rune(value)) > 2000 {
			return nil, fmt.Errorf("%w: learned outcome exceeds 2000 characters", ErrInvalidMemory)
		}
		if updated.Outcome == nil {
			updated.Outcome = &entity.CaseOutcome{}
		}
		updated.Outcome.Learned = value
		changed = true
	}
	if patch.Annotation != nil {
		value := strings.TrimSpace(*patch.Annotation)
		if len([]rune(value)) > 2000 {
			return nil, fmt.Errorf("%w: annotation exceeds 2000 characters", ErrInvalidMemory)
		}
		updated.Annotation = value
		changed = true
	}
	if patch.Tags != nil {
		tags, err := normalizeMemoryTags(patch.Tags)
		if err != nil {
			return nil, err
		}
		updated.Tags = tags
		changed = true
	}
	if !changed {
		return nil, ErrNoFields
	}

	if err := s.memRepo.Save(ctx, &updated); err != nil {
		return nil, err
	}
	if s.indexer != nil {
		if _, err := s.indexer.Store(ctx, &updated); err != nil {
			_ = s.memRepo.Save(ctx, current)
			return nil, fmt.Errorf("reindex updated memory: %w", err)
		}
	}
	return &updated, nil
}

// Delete removes a user-owned memory projection and its RAG chunks. If index
// deletion fails, the SQL projection is restored to avoid a half-deleted
// memory.
func (s *Service) Delete(ctx context.Context, userID int64, caseID string) error {
	if s.memRepo == nil || caseID == "" {
		return ErrMemoryNotFound
	}
	current, err := s.memRepo.Get(ctx, caseID)
	if err != nil || current == nil {
		return ErrMemoryNotFound
	}
	if !s.allowed(ctx, userID, current.CaseID) {
		return ErrForbidden
	}
	if err := s.memRepo.Delete(ctx, caseID); err != nil {
		return err
	}
	if s.indexer != nil {
		if err := s.indexer.DeleteSource(ctx, port.SourceCaseMemory, caseID); err != nil {
			_ = s.memRepo.Save(ctx, current)
			return fmt.Errorf("delete memory index: %w", err)
		}
	}
	return nil
}

func normalizeMemoryTags(tags []string) ([]string, error) {
	if len(tags) > 32 {
		return nil, fmt.Errorf("%w: more than 32 tags", ErrInvalidMemory)
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, fmt.Errorf("%w: tags cannot be empty", ErrInvalidMemory)
		}
		if len([]rune(tag)) > 64 {
			return nil, fmt.Errorf("%w: tag exceeds 64 characters", ErrInvalidMemory)
		}
		lower := strings.ToLower(tag)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, tag)
	}
	return out, nil
}

func cloneProjection(proj entity.CaseMemoryProjection) entity.CaseMemoryProjection {
	out := proj
	if proj.Outcome != nil {
		outcome := *proj.Outcome
		out.Outcome = &outcome
	}
	if len(proj.Tags) > 0 {
		out.Tags = append([]string(nil), proj.Tags...)
	}
	return out
}
