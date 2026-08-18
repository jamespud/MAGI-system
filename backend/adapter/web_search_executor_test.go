package magi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestWebSearchToolExecutorFailsOverToBrave(t *testing.T) {
	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer tavily.Close()

	var gotAuth, gotQuery string
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Subscription-Token")
		gotQuery = r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"web": map[string]any{
				"results": []map[string]string{
					{"title": "Brave result", "url": "https://brave.example", "description": "normalized"},
				},
			},
		})
	}))
	defer brave.Close()

	reg := metrics.New()
	executor, err := magi.NewWebSearchToolExecutor([]magi.WebSearchProviderSpec{
		{Provider: magi.SearchProviderTavily, APIKey: "tavily-key", BaseURL: tavily.URL},
		{Provider: magi.SearchProviderBrave, APIKey: "brave-key", BaseURL: brave.URL},
	}, reg)
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	result, err := executor.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"rust benchmarks"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotAuth != "brave-key" || gotQuery != "rust benchmarks" {
		t.Fatalf("brave request auth=%q query=%q", gotAuth, gotQuery)
	}
	if result.SourceURI != "https://brave.example" {
		t.Fatalf("source URI = %q", result.SourceURI)
	}
	parsed, ok := result.Structured.(*evidence.WebSearchResponse)
	if !ok || parsed.Provider != magi.SearchProviderBrave || len(parsed.Results) != 1 ||
		parsed.Results[0].Content != "normalized" {
		t.Fatalf("normalized response = %#v", result.Structured)
	}
	if reg.WebSearchFailovers.Load() != 1 {
		t.Fatalf("failover metric = %d, want 1", reg.WebSearchFailovers.Load())
	}
}

func TestWebSearchToolExecutorRejectsInvalidConfigurationAndQueries(t *testing.T) {
	if _, err := magi.NewWebSearchToolExecutor(nil, nil); err == nil {
		t.Fatal("expected empty provider chain error")
	}
	if _, err := magi.NewWebSearchToolExecutor([]magi.WebSearchProviderSpec{{Provider: "unknown", APIKey: "k"}}, nil); err == nil ||
		!strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unsupported provider error = %v", err)
	}

	executor, err := magi.NewWebSearchToolExecutor([]magi.WebSearchProviderSpec{
		{Provider: magi.SearchProviderBrave, APIKey: "k"},
	}, nil)
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	if _, err := executor.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"   "}`,
	}); err == nil || !strings.Contains(err.Error(), "query cannot be empty") {
		t.Fatalf("empty query error = %v", err)
	}
}

func TestWebSearchToolExecutorReturnsJoinedProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	executor, err := magi.NewWebSearchToolExecutor([]magi.WebSearchProviderSpec{
		{Provider: magi.SearchProviderTavily, APIKey: "a", BaseURL: server.URL},
		{Provider: magi.SearchProviderBrave, APIKey: "b", BaseURL: server.URL},
	}, nil)
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	_, err = executor.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"x"}`,
	})
	if err == nil || !errors.Is(err, err) {
		t.Fatalf("expected joined errors, got %v", err)
	}
	if !strings.Contains(err.Error(), "search provider tavily") ||
		!strings.Contains(err.Error(), "search provider brave") {
		t.Fatalf("joined error missing providers: %v", err)
	}
}

func TestWebSearchToolExecutorDoesNotRetryCanceledRequest(t *testing.T) {
	executor, err := magi.NewWebSearchToolExecutor([]magi.WebSearchProviderSpec{
		{Provider: magi.SearchProviderBrave, APIKey: "k", BaseURL: "http://invalid.invalid"},
		{Provider: magi.SearchProviderBrave, APIKey: "k2", BaseURL: "http://invalid.invalid"},
	}, metrics.New())
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Execute(ctx, port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"x"}`,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}
