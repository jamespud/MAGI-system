package rag

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// TestStoreRetrieveRoundTrip exercises the full Store -> Retrieve path with
// fake indexes (no Milvus/ES required). Verifies that a stored case's 300
// chunks are recalled and merged correctly.
func TestStoreRetrieveRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)

	// Fake vector index that returns ALL stored 300 IDs on search (so unanimity triggers).
	fv := &recordingVectorIndex{}
	fl := &FakeLexicalIndex{}

	ch := NewChunker(RuneTokenCounter{CharsPerToken: 1}, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	emb := FakeEmbedder{Dim: 3}
	retriever := NewRetriever(fv, fl, emb, repo, MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	adapter := NewHybridKnowledgeAdapter(ch, emb, repo, fv, fl, retriever, nil)

	proj := &entity.CaseMemoryProjection{
		CaseID:          "case-int",
		QuestionSummary: "Should we rewrite in Rust?",
		ContextSummary:  "Team has 2 Rust engineers and latency concerns.",
	}
	if _, err := adapter.Store(context.Background(), proj); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Recording vector index returns all stored 300s -> unanimous merges should fire.
	res, err := adapter.Retrieve(context.Background(), port.RetrieveRequest{Query: "rewrite rust", TopK: 15})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.Blocks) == 0 {
		t.Fatal("expected retrieval blocks, got 0")
	}
	for _, b := range res.Blocks {
		if b.Level != 300 && b.Level != 900 && b.Level != 1800 {
			t.Errorf("unexpected level %d", b.Level)
		}
	}
}

// recordingVectorIndex returns all 300 chunk IDs it has seen via Upsert.
type recordingVectorIndex struct {
	stored []VectorRecord
}

func (r *recordingVectorIndex) Upsert(ctx context.Context, recs []VectorRecord) error {
	r.stored = append(r.stored, recs...)
	return nil
}
func (r *recordingVectorIndex) Search(ctx context.Context, q []float32, topK int, f *IndexFilter) ([]VectorHit, error) {
	hits := make([]VectorHit, len(r.stored))
	for i, rec := range r.stored {
		hits[i] = VectorHit{ChunkID: rec.ChunkID, Score: 0.9}
	}
	return hits, nil
}
func (r *recordingVectorIndex) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	return nil
}
