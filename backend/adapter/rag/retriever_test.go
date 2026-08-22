package rag

import (
	"context"
	"errors"
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
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
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
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
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
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
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
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
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
	blocks, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
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
	_, err := r.Retrieve(context.Background(), "q", MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
	if err == nil {
		t.Fatal("expected error when both indexes fail")
	}
}

// perQueryLex returns distinct TextHits per query string (vector index is
// query-agnostic in these tests, so lexical alone proves per-query recall).
type perQueryLex struct{ byQuery map[string][]TextHit }

func (l *perQueryLex) Upsert(context.Context, []TextRecord) error { return nil }
func (l *perQueryLex) Search(_ context.Context, q string, _ int, _ *IndexFilter) ([]TextHit, error) {
	return l.byQuery[q], nil
}
func (l *perQueryLex) DeleteBySourceRef(context.Context, string, string) error { return nil }

func seedMultiQueryDB(t *testing.T) *ChunkRepository {
	t.Helper()
	db := newTestDB(t)
	repo := NewChunkRepository(db)
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
			{ID: "c5", Parent900ID: "p9b", Source: "case_memory", SourceRef: "case-1", Content: "l5", TokenCount: 300, Seq: 1},
			{ID: "c6", Parent900ID: "p9b", Source: "case_memory", SourceRef: "case-1", Content: "l6", TokenCount: 300, Seq: 2},
		},
	}
	if err := repo.WriteChunks(context.Background(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return repo
}

func TestRetrieverMultiQueryFusesBothQueries(t *testing.T) {
	repo := seedMultiQueryDB(t)
	lex := &perQueryLex{byQuery: map[string][]TextHit{
		"q1": {{ChunkID: "c1"}},
		"q2": {{ChunkID: "c4"}},
	}}
	// Vector index returns c2 for every query (query-agnostic fake).
	vec := &FakeVectorIndex{Hits: []VectorHit{{ChunkID: "c2"}}}
	r := NewRetriever(vec, lex, FakeEmbedder{Dim: 3}, repo,
		MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	blocks, err := r.RetrieveMulti(context.Background(),
		[]string{"q1", "q2"},
		MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
	if err != nil {
		t.Fatalf("RetrieveMulti: %v", err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		for _, id := range b.ChunkIDs {
			ids[id] = true
		}
	}
	for _, want := range []string{"c1", "c2", "c4"} {
		if !ids[want] {
			t.Errorf("multi-query fusion missing %s; got %v", want, ids)
		}
	}
}

func TestRRFManyFusesMultipleLists(t *testing.T) {
	a := []rrfHit{{id: "x1"}, {id: "x2"}}
	b := []rrfHit{{id: "x2"}, {id: "x3"}}
	out := rrfMany([][]rrfHit{a, b}, 60, 10)
	got := map[string]bool{}
	for _, h := range out {
		got[h.ChunkID] = true
	}
	if !got["x1"] || !got["x2"] || !got["x3"] {
		t.Errorf("rrfMany missing hits; got %v", got)
	}
}

// flakyVector fails its Nth Search call to simulate a degraded leg.
type flakyVector struct {
	FakeVectorIndex
	calls      int
	failOnCall int
}

func (f *flakyVector) Search(ctx context.Context, q []float32, topK int, fl *IndexFilter) ([]VectorHit, error) {
	f.calls++
	if f.failOnCall > 0 && f.calls == f.failOnCall {
		return nil, errors.New("vector index down")
	}
	return f.Hits, f.Err
}

func TestRetrieverMultiQueryDegradesWhenOneQueryFails(t *testing.T) {
	repo := seedMultiQueryDB(t)
	// q2's vector leg fails (2nd Search call); q2's lexical leg still works.
	flaky := &flakyVector{FakeVectorIndex: FakeVectorIndex{Hits: []VectorHit{{ChunkID: "c1"}}}, failOnCall: 2}
	lex := &perQueryLex{byQuery: map[string][]TextHit{
		"q1": {{ChunkID: "c2"}},
		"q2": {{ChunkID: "c4"}},
	}}
	r := NewRetriever(flaky, lex, FakeEmbedder{Dim: 3}, repo,
		MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	blocks, err := r.RetrieveMulti(context.Background(),
		[]string{"q1", "q2"},
		MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"}, nil)
	if err != nil {
		t.Fatalf("RetrieveMulti should degrade, got error: %v", err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		for _, id := range b.ChunkIDs {
			ids[id] = true
		}
	}
	for _, want := range []string{"c1", "c2", "c4"} {
		if !ids[want] {
			t.Errorf("degraded multi-query missing %s; got %v", want, ids)
		}
	}
}
