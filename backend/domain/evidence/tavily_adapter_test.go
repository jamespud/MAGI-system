package evidence_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestTavilyAdapter_SupportsOnlyWebSearch(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	webSearch := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{ToolName: "web_search"}}
	other := port.ToolDefinition{Name: "calc"}
	if !a.Supports(webSearch) {
		t.Fatal("should support web_search")
	}
	if a.Supports(other) {
		t.Fatal("should not support other tools")
	}
}

func TestTavilyAdapter_ExtractOnePerResult(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	resp := evidence.TavilyResponse{
		Answer: "yes",
		Results: []evidence.TavilyResult{
			{Title: "t1", URL: "https://a.example", Content: "content A", Score: 0.9},
			{Title: "t2", URL: "https://b.example", Content: "content B", Score: 0.8},
			{Title: "t3", URL: "https://c.example", Content: "content C", Score: 0.7},
		},
	}
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	cands, err := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Structured: &resp})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(cands))
	}
	if cands[0].Observation != "content A" || cands[0].SourceURI != "https://a.example" {
		t.Fatalf("candidate 0: %+v", cands[0])
	}
	for _, c := range cands {
		if c.Reliability.Final <= 0 {
			t.Fatalf("reliability not computed: %+v", c.Reliability)
		}
	}
}

func TestTavilyAdapter_ExtractFallsBackToRawWhenNoStructured(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	cands, err := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Output: `{"answer":"","results":[]}`})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 raw fallback candidate, got %d", len(cands))
	}
}

func TestTavilyAdapter_ExtractFromOutputJSON(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	body, _ := json.Marshal(evidence.TavilyResponse{
		Results: []evidence.TavilyResult{{URL: "https://x.example", Content: "X"}},
	})
	cands, _ := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Output: string(body)})
	if len(cands) != 1 || cands[0].SourceURI != "https://x.example" {
		t.Fatalf("expected 1 candidate from output JSON, got %+v", cands)
	}
}
