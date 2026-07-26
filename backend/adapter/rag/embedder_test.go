package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedderEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		inputs, _ := req["input"].([]any)
		data := []map[string]any{}
		for range inputs {
			data = append(data, map[string]any{"embedding": []float64{0.1, 0.2, 0.3}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedder(srv.URL, "key", "bge-m3", 3)
	vecs, err := emb.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("vecs = %d, want 2", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Errorf("vec dim = %d, want 3", len(vecs[0]))
	}
}
