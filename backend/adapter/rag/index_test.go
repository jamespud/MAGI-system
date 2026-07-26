package rag

import (
	"context"
	"testing"
)

func TestFakeVectorIndexRoundTrip(t *testing.T) {
	fv := &FakeVectorIndex{Hits: []VectorHit{{ChunkID: "c300_1", Score: 0.9}}}
	hits, err := fv.Search(context.Background(), []float32{0.1, 0.2}, 15, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "c300_1" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestFakeLexicalIndexRoundTrip(t *testing.T) {
	fl := &FakeLexicalIndex{Hits: []TextHit{{ChunkID: "c300_2", Score: 12.0}}}
	hits, err := fl.Search(context.Background(), "query", 15, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "c300_2" {
		t.Errorf("hits = %+v", hits)
	}
}
