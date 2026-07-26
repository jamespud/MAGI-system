package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// ESIndexer implements LexicalIndex over Elasticsearch. Index mapping:
// chunk_id (keyword), content (text), source (keyword), source_ref (keyword).
type ESIndexer struct {
	client *elasticsearch.Client
	index  string
}

func NewESIndexer(addresses []string, index string) (*ESIndexer, error) {
	cli, err := elasticsearch.NewClient(elasticsearch.Config{Addresses: addresses})
	if err != nil {
		return nil, fmt.Errorf("es client: %w", err)
	}
	idx := &ESIndexer{client: cli, index: index}
	if err := idx.ensureIndex(context.Background()); err != nil {
		return nil, err
	}
	return idx, nil
}

func (e *ESIndexer) ensureIndex(ctx context.Context) error {
	res, err := esapi.IndicesExistsRequest{Index: []string{e.index}}.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("es exists: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == 200 {
		return nil
	}
	mapping := `{"mappings":{"properties":{"chunk_id":{"type":"keyword"},"content":{"type":"text"},"source":{"type":"keyword"},"source_ref":{"type":"keyword"}}}}`
	req := esapi.IndicesCreateRequest{Index: e.index, Body: strings.NewReader(mapping)}
	r, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("es create index: %w", err)
	}
	defer r.Body.Close()
	return nil
}

func (e *ESIndexer) Upsert(ctx context.Context, recs []TextRecord) error {
	for _, r := range recs {
		body, _ := json.Marshal(map[string]any{"chunk_id": r.ChunkID, "content": r.Content, "source": r.Source, "source_ref": r.SourceRef})
		req := esapi.IndexRequest{Index: e.index, DocumentID: r.ChunkID, Body: bytes.NewReader(body), Refresh: "true"}
		res, err := req.Do(ctx, e.client)
		if err != nil {
			return fmt.Errorf("es upsert: %w", err)
		}
		res.Body.Close()
	}
	return nil
}

func (e *ESIndexer) Search(ctx context.Context, query string, topK int, f *IndexFilter) ([]TextHit, error) {
	q := map[string]any{"query": map[string]any{"match": map[string]any{"content": query}}}
	if f != nil && len(f.Sources) > 0 {
		terms := make([]map[string]any, len(f.Sources))
		for i, s := range f.Sources {
			terms[i] = map[string]any{"term": map[string]any{"source": s}}
		}
		q = map[string]any{"query": map[string]any{"bool": map[string]any{"must": map[string]any{"match": map[string]any{"content": query}}, "filter": terms}}}
	}
	body, _ := json.Marshal(q)
	req := esapi.SearchRequest{Index: []string{e.index}, Body: bytes.NewReader(body), Size: &topK}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return nil, fmt.Errorf("es search: %w", err)
	}
	defer res.Body.Close()
	var out struct {
		Hits struct {
			Hits []struct {
				Score  float64 `json:"_score"`
				Source struct {
					ChunkID string `json:"chunk_id"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	hits := make([]TextHit, 0, len(out.Hits.Hits))
	for _, h := range out.Hits.Hits {
		hits = append(hits, TextHit{ChunkID: h.Source.ChunkID, Score: h.Score})
	}
	return hits, nil
}

func (e *ESIndexer) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	q := map[string]any{"query": map[string]any{"term": map[string]any{"source_ref": sourceRef}}}
	body, _ := json.Marshal(q)
	req := esapi.DeleteByQueryRequest{Index: []string{e.index}, Body: bytes.NewReader(body)}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("es delete: %w", err)
	}
	res.Body.Close()
	return nil
}
