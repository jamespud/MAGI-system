package rag

import (
	"context"
	"fmt"
	"testing"
)

func newRetrieverForTest(t *testing.T, vecHits []VectorHit, lexHits []TextHit) *Retriever {
	return newRetrieverForTestFull(t, vecHits, nil, lexHits, nil)
}

func newRetrieverForTestFull(t *testing.T, vecHits []VectorHit, vecErr error, lexHits []TextHit, lexErr error) *Retriever {
	t.Helper()
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	// Seed hierarchy: one 1800 -> two 900 (a,b) -> three 300 each.
	// 900a: c1,c2,c3 ; 900b: c4,c5,c6
	doc := ChunkedDoc{
		Chunks1800: []ChunkBlock{{ID: "p18", Source: "case_memory", SourceRef: "case-1", Content: "top", TokenCount: 1800}},
		Chunks900: []ChunkBlock{
			{ID: "p9a", Parent1800ID: "p18", Source: "case_memory", SourceRef: "case-1", Content: "mida", TokenCount: 900, Seq: 0},
			{ID: "p9b", Parent1800ID: "p18", Source: "case_memory", SourceRef: "case-1", Content: "midb", TokenCount: 900, Seq: 1},
		},
		Chunks300: []ChunkBlock{
			{ID: "c1", Parent900ID: "p9a", Source: "case_memory", SourceRef: "case-1", Content: "l1", TokenCount: 300, Seq: 0},
			{ID: "c2", Parent900ID: "p9a", Source: "case_memory", SourceRef: "case-1", Content: "l2", TokenCount: 300, Seq: 1},
			{ID: "c3", Parent900ID: "p9a", Source: "case_memory", SourceRef: "case-1", Content: "l3", TokenCount: 300, Seq: 2},
			{ID: "c4", Parent900ID: "p9b", Source: "case_memory", SourceRef: "case-1", Content: "l4", TokenCount: 300, Seq: 0},
		},
	}
	if err := repo.WriteChunks(context.Background(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return &Retriever{
		vec:  &FakeVectorIndex{Hits: vecHits, Err: vecErr},
		lex:  &FakeLexicalIndex{Hits: lexHits, Err: lexErr},
		emb:  FakeEmbedder{Dim: 3},
		repo: repo,
		opts: MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"},
	}
}

func TestRetrieverUnanimousMergeTo900(t *testing.T) {
	// All 3 of 900a's children (c1,c2,c3) recalled -> pull 900a, discard 300s.
	r := newRetrieverForTest(t, []VectorHit{{ChunkID: "c1"}, {ChunkID: "c2"}, {ChunkID: "c3"}}, nil)
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	found900 := false
	for _, b := range blocks {
		if b.Level == 900 {
			found900 = true
			if b.Content != "mida" {
				t.Errorf("900 content = %q, want mida", b.Content)
			}
		}
		if b.Level == 300 {
			t.Errorf("expected 300s discarded, got block %+v", b)
		}
	}
	if !found900 {
		t.Error("expected a 900 block from unanimous merge")
	}
}

func TestRetrieverOrphanKeep300(t *testing.T) {
	// Only c1 recalled (no unanimity) -> keep c1 as 300.
	r := newRetrieverForTest(t, []VectorHit{{ChunkID: "c1"}}, nil)
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Level != 300 || blocks[0].Content != "l1" {
		t.Errorf("blocks = %+v, want single 300 l1", blocks)
	}
}

func TestRetrieverRRFFusion(t *testing.T) {
	// Vector returns c1; ES returns c4 (different). Both should be fused (orphans).
	r := newRetrieverForTest(t, []VectorHit{{ChunkID: "c1"}}, []TextHit{{ChunkID: "c4"}})
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		for _, id := range b.ChunkIDs {
			ids[id] = true
		}
	}
	if !ids["c1"] || !ids["c4"] {
		t.Errorf("RRF fusion missed chunks; got ids %v", ids)
	}
}

func TestRetrieverVectorDegradedFallsBackToLexical(t *testing.T) {
	// Milvus down (collection not loaded / outage): ES hits must still return.
	r := newRetrieverForTestFull(t, nil, fmt.Errorf("collection not loaded"), []TextHit{{ChunkID: "c4"}}, nil)
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err != nil {
		t.Fatalf("retrieve should fall back to lexical, got error: %v", err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		for _, id := range b.ChunkIDs {
			ids[id] = true
		}
	}
	if !ids["c4"] {
		t.Errorf("expected lexical hit c4 after vector degradation, got %v", ids)
	}
}

func TestRetrieverLexicalDegradedFallsBackToVector(t *testing.T) {
	// ES down: vector hits must still return.
	r := newRetrieverForTestFull(t, []VectorHit{{ChunkID: "c1"}}, nil, nil, fmt.Errorf("es connection refused"))
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err != nil {
		t.Fatalf("retrieve should fall back to vector, got error: %v", err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		for _, id := range b.ChunkIDs {
			ids[id] = true
		}
	}
	if !ids["c1"] {
		t.Errorf("expected vector hit c1 after lexical degradation, got %v", ids)
	}
}

func TestRetrieverBothIndexesFail(t *testing.T) {
	// Both backends down: retrieve must surface the error, not silently return empty.
	r := newRetrieverForTestFull(t, nil, fmt.Errorf("milvus down"), nil, fmt.Errorf("es down"))
	_, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	if err == nil {
		t.Fatal("expected error when both indexes fail")
	}
}
