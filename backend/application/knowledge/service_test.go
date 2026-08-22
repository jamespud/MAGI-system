package knowledge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/knowledge"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memKnowRepo struct {
	byID    map[string]*entity.KnowledgeDoc
	order   []string
	deleted map[string]bool
}

func newMemKnowRepo() *memKnowRepo {
	return &memKnowRepo{byID: map[string]*entity.KnowledgeDoc{}, deleted: map[string]bool{}}
}

func (r *memKnowRepo) Create(ctx context.Context, doc *entity.KnowledgeDoc) error {
	r.byID[doc.ID] = doc
	r.order = append(r.order, doc.ID)
	return nil
}
func (r *memKnowRepo) Get(ctx context.Context, id string) (*entity.KnowledgeDoc, error) {
	d, ok := r.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}
func (r *memKnowRepo) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.KnowledgeDoc, error) {
	var out []*entity.KnowledgeDoc
	for _, id := range r.order {
		d := r.byID[id]
		if userID != 0 && d.UserID != 0 && d.UserID != userID {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}
func (r *memKnowRepo) Update(ctx context.Context, doc *entity.KnowledgeDoc) error {
	r.byID[doc.ID] = doc
	return nil
}
func (r *memKnowRepo) Delete(ctx context.Context, id string) error {
	r.deleted[id] = true
	delete(r.byID, id)
	return nil
}

func (r *memKnowRepo) ListAll(_ context.Context) ([]*entity.KnowledgeDoc, error) {
	out := make([]*entity.KnowledgeDoc, 0, len(r.byID))
	for _, d := range r.byID {
		out = append(out, d)
	}
	return out, nil
}

type fakeDocIndexer struct {
	storeErr    error
	deletedSrc  []string
	lastChunks  int
	storeCalled []string
}

func (f *fakeDocIndexer) StoreDocument(ctx context.Context, doc *entity.KnowledgeDoc) (port.StoreStats, error) {
	f.storeCalled = append(f.storeCalled, doc.ID)
	if f.storeErr != nil {
		return port.StoreStats{}, f.storeErr
	}
	f.lastChunks = 7
	return port.StoreStats{Chunks300: 7}, nil
}
func (f *fakeDocIndexer) DeleteSource(ctx context.Context, source, sourceRef string) error {
	f.deletedSrc = append(f.deletedSrc, source+":"+sourceRef)
	return nil
}

func TestKnowledgeService_CreateIndexes(t *testing.T) {
	repo := newMemKnowRepo()
	idx := &fakeDocIndexer{}
	svc := knowledge.NewService(repo, idx)

	doc, err := svc.Create(context.Background(), 7, "Rust migration", "Move the storage layer to Rust.", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if doc.Status != entity.KnowledgeStatusIndexed {
		t.Fatalf("status = %s, want indexed", doc.Status)
	}
	if doc.Chunks != 7 {
		t.Fatalf("chunks = %d, want 7", doc.Chunks)
	}
	if doc.UserID != 7 || doc.SourceKind != entity.KnowledgeSourceText {
		t.Fatalf("doc = %+v", doc)
	}
	if len(idx.storeCalled) != 1 || idx.storeCalled[0] != doc.ID {
		t.Fatalf("indexer called: %v", idx.storeCalled)
	}
}

func TestKnowledgeService_CreateFailureKeepsDoc(t *testing.T) {
	repo := newMemKnowRepo()
	idx := &fakeDocIndexer{storeErr: errors.New("embedder down")}
	svc := knowledge.NewService(repo, idx)

	doc, err := svc.Create(context.Background(), 7, "T", "content", "", "")
	if err != nil {
		t.Fatalf("create should not fail when indexing fails: %v", err)
	}
	if doc.Status != entity.KnowledgeStatusFailed {
		t.Fatalf("status = %s, want failed", doc.Status)
	}
	if doc.Error == "" {
		t.Fatal("expected error recorded on doc")
	}
	// The doc must still be persisted.
	if _, ok := repo.byID[doc.ID]; !ok {
		t.Fatal("doc should be persisted even when indexing fails")
	}
}

func TestKnowledgeService_CreateValidation(t *testing.T) {
	repo := newMemKnowRepo()
	svc := knowledge.NewService(repo, &fakeDocIndexer{})
	if _, err := svc.Create(context.Background(), 7, "", "c", "", ""); err == nil {
		t.Fatal("expected error for empty title")
	}
	if _, err := svc.Create(context.Background(), 7, "t", "", "", ""); err == nil {
		t.Fatal("expected error for empty content (text source)")
	}
	if _, err := svc.Create(context.Background(), 7, "t", "c", "pdf", ""); err == nil {
		t.Fatal("expected error for invalid source_kind")
	}
}

func TestKnowledgeService_GetAndDeleteOwnerScoped(t *testing.T) {
	repo := newMemKnowRepo()
	idx := &fakeDocIndexer{}
	svc := knowledge.NewService(repo, idx)

	doc, _ := svc.Create(context.Background(), 7, "A", "content a", "", "")
	other, _ := svc.Create(context.Background(), 8, "B", "content b", "", "")

	// user 7 can read own doc, not user 8's.
	if _, err := svc.Get(context.Background(), 7, doc.ID); err != nil {
		t.Fatalf("get own: %v", err)
	}
	if _, err := svc.Get(context.Background(), 7, other.ID); err != knowledge.ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
	// open mode (user 0) can read anything.
	if _, err := svc.Get(context.Background(), 0, other.ID); err != nil {
		t.Fatalf("open-mode get: %v", err)
	}

	// delete purges index then removes row.
	if err := svc.Delete(context.Background(), 7, doc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(idx.deletedSrc) != 1 || idx.deletedSrc[0] != port.SourceKnowledgeDoc+":"+doc.ID {
		t.Fatalf("index purge = %v", idx.deletedSrc)
	}
	if !repo.deleted[doc.ID] {
		t.Fatal("doc should be deleted from repo")
	}
	// cannot delete another user's doc.
	if err := svc.Delete(context.Background(), 7, other.ID); err != knowledge.ErrForbidden {
		t.Fatalf("expected forbidden delete, got %v", err)
	}
}

func TestKnowledgeService_ListFiltersByUser(t *testing.T) {
	repo := newMemKnowRepo()
	svc := knowledge.NewService(repo, nil)
	_, _ = svc.Create(context.Background(), 7, "A", "a", "", "")
	_, _ = svc.Create(context.Background(), 8, "B", "b", "", "")

	docs, err := svc.List(context.Background(), 7, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 1 || docs[0].Title != "A" {
		t.Fatalf("user list = %+v", docs)
	}
}
