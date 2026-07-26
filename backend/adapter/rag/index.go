package rag

import "context"

// IndexFilter narrows an index search by source metadata.
type IndexFilter struct {
	Sources    []string
	SourceRefs []string
}

// VectorRecord is a 300-level vector to upsert into the vector index.
type VectorRecord struct {
	ChunkID   string
	Embedding []float32
	Source    string
	SourceRef string
}

// VectorHit is a vector search result.
type VectorHit struct {
	ChunkID string
	Score   float32
}

// TextRecord is a 300-level text to upsert into the lexical index.
type TextRecord struct {
	ChunkID   string
	Content   string
	Source    string
	SourceRef string
}

// TextHit is a lexical (BM25) search result.
type TextHit struct {
	ChunkID string
	Score   float64
}

// VectorIndex is the vector store (Milvus). Only 300-level vectors.
type VectorIndex interface {
	Upsert(ctx context.Context, recs []VectorRecord) error
	Search(ctx context.Context, queryVec []float32, topK int, f *IndexFilter) ([]VectorHit, error)
	DeleteBySourceRef(ctx context.Context, source, sourceRef string) error
}

// LexicalIndex is the BM25 store (Elasticsearch). Only 300-level text.
type LexicalIndex interface {
	Upsert(ctx context.Context, recs []TextRecord) error
	Search(ctx context.Context, query string, topK int, f *IndexFilter) ([]TextHit, error)
	DeleteBySourceRef(ctx context.Context, source, sourceRef string) error
}

// FakeVectorIndex is an in-memory VectorIndex for tests.
type FakeVectorIndex struct {
	Hits []VectorHit
	Err  error
}

func (f *FakeVectorIndex) Upsert(ctx context.Context, recs []VectorRecord) error { return nil }
func (f *FakeVectorIndex) Search(ctx context.Context, q []float32, topK int, fl *IndexFilter) ([]VectorHit, error) {
	return f.Hits, f.Err
}
func (f *FakeVectorIndex) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	return nil
}

// FakeLexicalIndex is an in-memory LexicalIndex for tests.
type FakeLexicalIndex struct {
	Hits []TextHit
	Err  error
}

func (f *FakeLexicalIndex) Upsert(ctx context.Context, recs []TextRecord) error { return nil }
func (f *FakeLexicalIndex) Search(ctx context.Context, q string, topK int, fl *IndexFilter) ([]TextHit, error) {
	return f.Hits, f.Err
}
func (f *FakeLexicalIndex) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	return nil
}
