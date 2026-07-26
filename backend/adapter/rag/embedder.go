package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Embedder turns text into vectors via an OpenAI-compatible /embeddings endpoint.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type OpenAIEmbedder struct {
	baseURL   string
	apiKey    string
	modelName string
	dim       int
	client    *http.Client
}

func NewOpenAIEmbedder(baseURL, apiKey, modelName string, dim int) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		baseURL: baseURL, apiKey: apiKey, modelName: modelName, dim: dim,
		client: http.DefaultClient,
	}
}

type embeddingsResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model": e.modelName,
		"input": texts,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: status %d", resp.StatusCode)
	}
	var r embeddingsResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	out := make([][]float32, len(r.Data))
	for i, d := range r.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// FakeEmbedder is a deterministic embedder for tests. It maps each text to a
// constant vector of the given dim.
type FakeEmbedder struct{ Dim int }

func (f FakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, f.Dim)
		for j := range v {
			v[j] = float32(i) / 10.0
		}
		out[i] = v
	}
	return out, nil
}
