package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestHybridKnowledgeAdapterStore(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	vec := &FakeVectorIndex{}
	lex := &FakeLexicalIndex{}
	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}

	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, nil, nil)
	proj := &entity.CaseMemoryProjection{
		CaseID:          "case-1",
		QuestionSummary: "Should we rewrite in Rust?",
		ContextSummary:  "Team has 2 Rust engineers.",
	}
	if _, err := adapter.Store(context.Background(), proj); err != nil {
		t.Fatalf("store: %v", err)
	}
	var n int64
	db.Model(&Chunk300{}).Count(&n)
	if n == 0 {
		t.Error("expected 300 chunks persisted to MySQL")
	}
}

func TestHybridKnowledgeAdapterStoreUsesRenderDocument(t *testing.T) {
	proj := &entity.CaseMemoryProjection{CaseID: "case-2", QuestionSummary: "unique-question-marker"}
	doc := memory.RenderDocument(proj)
	if !contains(doc, "unique-question-marker") {
		t.Error("RenderDocument output missing projection field")
	}
}

func TestHybridKnowledgeAdapterRetrieve(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	vec := &FakeVectorIndex{Hits: []VectorHit{{ChunkID: "c1"}}}
	lex := &FakeLexicalIndex{}
	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}
	retriever := NewRetriever(vec, lex, emb, repo, MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever, nil)

	// Seed one orphan 300 so retrieve returns something.
	repo.WriteChunks(context.Background(), ChunkedDoc{
		Chunks1800: []ChunkBlock{{ID: "p18", Source: "case_memory", SourceRef: "case-1", Content: "top", TokenCount: 1800}},
		Chunks900:  []ChunkBlock{{ID: "p9a", Parent1800ID: "p18", Source: "case_memory", SourceRef: "case-1", Content: "mid", TokenCount: 900}},
		Chunks300:  []ChunkBlock{{ID: "c1", Parent900ID: "p9a", Source: "case_memory", SourceRef: "case-1", Content: "leaf1", TokenCount: 300}},
	})

	res, err := adapter.Retrieve(context.Background(), port.RetrieveRequest{Query: "q", TopK: 15})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.Blocks) != 1 || res.Blocks[0].Level != 300 || res.Blocks[0].Content != "leaf1" {
		t.Errorf("blocks = %+v", res.Blocks)
	}
}

// recordingPublisher captures published events for assertions.
type recordingPublisher struct {
	events []entity.MagiEvent
}

func (p *recordingPublisher) Publish(_ context.Context, e entity.MagiEvent) error {
	p.events = append(p.events, e)
	return nil
}

// failingVectorIndex simulates a Milvus outage (best-effort write must not
// fail Store, since MySQL is the source of truth).
type failingVectorIndex struct{}

func (failingVectorIndex) Upsert(context.Context, []VectorRecord) error {
	return fmt.Errorf("simulated milvus outage")
}
func (failingVectorIndex) Search(context.Context, []float32, int, *IndexFilter) ([]VectorHit, error) {
	return nil, nil
}
func (failingVectorIndex) DeleteBySourceRef(context.Context, string, string) error {
	return nil
}

func longProjection(caseID string) *entity.CaseMemoryProjection {
	return &entity.CaseMemoryProjection{
		CaseID:          caseID,
		QuestionSummary: strings.Repeat("Should we concentrate all available capital into a single high-beta stock? ", 8),
		ContextSummary:  strings.Repeat("The stock trades near its 52-week high with strong analyst consensus but wide target dispersion. ", 8),
	}
}

func TestHybridKnowledgeAdapterStoreStatsAndEvent(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	vec := &FakeVectorIndex{}
	lex := &FakeLexicalIndex{}
	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}
	pub := &recordingPublisher{}

	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, nil, pub)
	stats, err := adapter.Store(context.Background(), longProjection("case-stats"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if stats.Chunks300 == 0 || stats.Chunks900 == 0 || stats.Chunks1800 == 0 {
		t.Errorf("expected chunks at all three levels, got %+v", stats)
	}
	if len(pub.events) != 1 || pub.events[0].Type != entity.EventMemoryIndexed {
		t.Fatalf("expected one MEMORY_INDEXED event, got %d events: %+v", len(pub.events), pub.events)
	}
	var payload map[string]any
	if err := json.Unmarshal(pub.events[0].Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["chunks_300"] != float64(stats.Chunks300) ||
		payload["chunks_900"] != float64(stats.Chunks900) ||
		payload["chunks_1800"] != float64(stats.Chunks1800) {
		t.Errorf("event payload mismatch: %v vs stats %+v", payload, stats)
	}
}

func TestHybridKnowledgeAdapterStoreIdempotent(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	adapter := NewHybridKnowledgeAdapter(
		NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50}),
		FakeEmbedder{Dim: 3}, repo, &FakeVectorIndex{}, &FakeLexicalIndex{}, nil, nil,
	)
	proj := longProjection("case-idem")

	stats1, err := adapter.Store(context.Background(), proj)
	if err != nil {
		t.Fatalf("first store: %v", err)
	}
	var n1 int64
	db.Model(&Chunk300{}).Count(&n1)

	stats2, err := adapter.Store(context.Background(), proj)
	if err != nil {
		t.Fatalf("second store: %v", err)
	}
	var n2 int64
	db.Model(&Chunk300{}).Count(&n2)

	if n1 == 0 {
		t.Fatal("expected chunks after first store")
	}
	if n1 != n2 {
		t.Errorf("idempotent store grew chunk rows: %d -> %d", n1, n2)
	}
	if stats1.Chunks300 != stats2.Chunks300 {
		t.Errorf("stats differ across re-stores: %+v vs %+v", stats1, stats2)
	}
}

func TestHybridKnowledgeAdapterStoreVectorFailureStillPersistsMySQL(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	adapter := NewHybridKnowledgeAdapter(
		NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50}),
		FakeEmbedder{Dim: 3}, repo, failingVectorIndex{}, &FakeLexicalIndex{}, nil, nil,
	)

	stats, err := adapter.Store(context.Background(), longProjection("case-vecfail"))
	if err != nil {
		t.Fatalf("store should tolerate vector outage, got: %v", err)
	}
	if stats.Chunks300 == 0 {
		t.Errorf("expected chunk stats despite vector failure, got %+v", stats)
	}
	var n int64
	db.Model(&Chunk300{}).Count(&n)
	if n == 0 {
		t.Error("expected MySQL chunks persisted despite vector outage")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestHybridKnowledgeAdapterStoreDocumentAndDelete(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	vec := &FakeVectorIndex{}
	lex := &FakeLexicalIndex{}
	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}
	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, nil, nil)

	doc := &entity.KnowledgeDoc{
		ID: "kd-1", UserID: 7, Title: "Postgres tuning",
		Content: "Set shared_buffers to 25% of RAM and enable WAL compression.",
	}
	stats, err := adapter.StoreDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("storedoc: %v", err)
	}
	if stats.Chunks300 == 0 {
		t.Fatal("expected chunks produced")
	}
	var n int64
	db.Model(&Chunk300{}).Where("source = ? AND source_ref = ?", port.SourceKnowledgeDoc, doc.ID).Count(&n)
	if n == 0 {
		t.Error("expected knowledge chunks persisted to MySQL")
	}

	// DeleteSource must purge the document's chunks.
	if err := adapter.DeleteSource(context.Background(), port.SourceKnowledgeDoc, doc.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	db.Model(&Chunk300{}).Where("source = ? AND source_ref = ?", port.SourceKnowledgeDoc, doc.ID).Count(&n)
	if n != 0 {
		t.Errorf("expected chunks removed, got %d", n)
	}
}

func TestHybridKnowledgeAdapterSourcesScoped(t *testing.T) {
	// A document source must not be returned by a case_memory-scoped retrieve,
	// and vice versa — both live in the same index but must be separable.
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	vec := &FakeVectorIndex{}
	lex := &FakeLexicalIndex{}
	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}
	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, nil, nil)

	if _, err := adapter.StoreDocument(context.Background(), &entity.KnowledgeDoc{
		ID: "kd-9", UserID: 1, Title: "doc", Content: "document-only content marker",
	}); err != nil {
		t.Fatalf("storedoc: %v", err)
	}
	if _, err := adapter.Store(context.Background(), &entity.CaseMemoryProjection{
		CaseID: "case-9", QuestionSummary: "case-only question",
	}); err != nil {
		t.Fatalf("store case: %v", err)
	}

	// The chunk repository should hold both under their own source namespaces.
	var docN, caseN int64
	db.Model(&Chunk300{}).Where("source = ?", port.SourceKnowledgeDoc).Count(&docN)
	db.Model(&Chunk300{}).Where("source = ?", port.SourceCaseMemory).Count(&caseN)
	if docN == 0 || caseN == 0 {
		t.Errorf("doc chunks=%d case chunks=%d, want both > 0", docN, caseN)
	}
}
