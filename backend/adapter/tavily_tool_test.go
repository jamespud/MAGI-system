package magi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestTavilyToolExecutor_CallsApiAndParses(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(evidence.TavilyResponse{
			Answer: "yes",
			Results: []evidence.TavilyResult{
				{Title: "t1", URL: "https://a.example", Content: "A", Score: 0.9},
				{Title: "t2", URL: "https://b.example", Content: "B", Score: 0.8},
			},
		})
	}))
	defer srv.Close()

	exec := magi.NewTavilyToolExecutorWithURL("test-key", srv.URL)
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"rust benchmarks"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotBody["api_key"] != "test-key" {
		t.Fatalf("api_key not sent: %+v", gotBody)
	}
	if gotBody["query"] != "rust benchmarks" {
		t.Fatalf("query not sent: %+v", gotBody)
	}
	tr, ok := res.Structured.(*evidence.TavilyResponse)
	if !ok || tr == nil || len(tr.Results) != 2 {
		t.Fatalf("Structured not parsed: %+v", res.Structured)
	}
	if res.SourceURI != "https://a.example" {
		t.Fatalf("SourceURI: %s", res.SourceURI)
	}
}

func TestTavilyToolExecutor_ReturnsErrorOnHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	exec := magi.NewTavilyToolExecutorWithURL("k", srv.URL)
	_, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"x"}`,
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestLocalToolRegistry_ListReturnsWebSearchDef(t *testing.T) {
	reg := magi.NewLocalToolRegistry()
	defs, err := reg.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceLocal, ToolName: "web_search"},
		{Source: entity.ToolSourceLocal, ToolName: "unknown_tool"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "web_search" {
		t.Fatalf("expected 1 web_search def, got %+v", defs)
	}
}

// Compile-time: implementations satisfy the ports.
var _ port.ToolRegistryPort = (*magi.LocalToolRegistry)(nil)
var _ port.ToolExecutorPort = (*magi.TavilyToolExecutor)(nil)
