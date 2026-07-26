package rag

import (
	"context"
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

	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, nil)
	proj := &entity.CaseMemoryProjection{
		CaseID:          "case-1",
		QuestionSummary: "Should we rewrite in Rust?",
		ContextSummary:  "Team has 2 Rust engineers.",
	}
	if err := adapter.Store(context.Background(), proj); err != nil {
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
	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever)

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
