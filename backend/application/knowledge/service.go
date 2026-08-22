// Package knowledge manages user-uploaded knowledge documents: persistence,
// ownership, and indexing into the RAG pipeline for agent retrieval.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ErrForbidden is returned when a principal tries to access a document they
// do not own.
var ErrForbidden = errors.New("forbidden")

// ErrNotFound is returned when a document does not exist.
var ErrNotFound = errors.New("knowledge doc not found")

// Service is the application-layer service for knowledge documents.
type Service struct {
	repo port.KnowledgeRepository
	idx  port.DocumentIndexer
}

// NewService creates a KnowledgeService. idx may be nil (no RAG configured),
// in which case documents are stored but not vector-indexed.
func NewService(repo port.KnowledgeRepository, idx port.DocumentIndexer) *Service {
	return &Service{repo: repo, idx: idx}
}

// Create persists a document and indexes it into the RAG pipeline. A failed
// index never loses the user's content: the document is stored with
// Status=failed and the error recorded so the UI can surface it.
func (s *Service) Create(ctx context.Context, userID int64, title, content, sourceKind, sourceURL string) (*entity.KnowledgeDoc, error) {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" {
		return nil, fmt.Errorf("knowledge: title is required")
	}
	if content == "" && sourceKind != entity.KnowledgeSourceURL {
		return nil, fmt.Errorf("knowledge: content is required")
	}
	if sourceKind == "" {
		sourceKind = entity.KnowledgeSourceText
	}
	if sourceKind != entity.KnowledgeSourceText && sourceKind != entity.KnowledgeSourceURL {
		return nil, fmt.Errorf("knowledge: source_kind must be %q or %q", entity.KnowledgeSourceText, entity.KnowledgeSourceURL)
	}

	doc := &entity.KnowledgeDoc{
		ID:         fmt.Sprintf("kd-%s", uuid.NewString()),
		UserID:     userID,
		Title:      title,
		Content:    content,
		SourceKind: sourceKind,
		SourceURL:  strings.TrimSpace(sourceURL),
		// Async (durable) indexing completes in the background; the poller
		// writes back the final status/chunks. Sync adapters set it below.
		Status: entity.KnowledgeStatusPending,
	}
	if s.repo == nil {
		return nil, fmt.Errorf("knowledge: repository is not configured")
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return nil, fmt.Errorf("knowledge: persist: %w", err)
	}

	if s.idx != nil {
		stats, err := s.idx.StoreDocument(ctx, doc)
		if err != nil {
			doc.Status = entity.KnowledgeStatusFailed
			doc.Error = err.Error()
			_ = s.repo.Update(ctx, doc)
			return doc, nil
		}
		// A durable adapter returns zero stats (it only enqueues), so status
		// stays pending until the poller writes indexed/failed. A sync adapter
		// returns real stats and finalizes immediately.
		if stats.Chunks300 > 0 {
			doc.Status = entity.KnowledgeStatusIndexed
			doc.Chunks = stats.Chunks300
			doc.Error = ""
			_ = s.repo.Update(ctx, doc)
		}
	}
	return doc, nil
}

// List returns the documents a user may access (owned or, in open mode, all).
func (s *Service) List(ctx context.Context, userID int64, limit, offset int) ([]*entity.KnowledgeDoc, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.ListByUser(ctx, userID, limit, offset)
}

// Get returns one document the user may access.
func (s *Service) Get(ctx context.Context, userID int64, id string) (*entity.KnowledgeDoc, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	doc, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	if !s.owns(userID, doc) {
		return nil, ErrForbidden
	}
	return doc, nil
}

// Delete removes a document the user owns, first purging its RAG chunks.
func (s *Service) Delete(ctx context.Context, userID int64, id string) error {
	if s.repo == nil {
		return ErrNotFound
	}
	doc, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.idx != nil {
		if derr := s.idx.DeleteSource(ctx, port.SourceKnowledgeDoc, doc.ID); derr != nil {
			return fmt.Errorf("knowledge: purge index: %w", derr)
		}
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) owns(userID int64, doc *entity.KnowledgeDoc) bool {
	return userID == 0 || doc.UserID == 0 || doc.UserID == userID
}
