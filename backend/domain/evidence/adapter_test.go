package evidence_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestNativeAdapter_StructuredOutput(t *testing.T) {
	a := evidence.NewNativeAdapter(evidence.DefaultReliabilityResolver())
	tool := port.ToolDefinition{Binding: entity.ToolBinding{Source: entity.ToolSourceLocal}}
	result := &port.ToolExecutionResult{Output: "raw", Structured: map[string]any{"score": 42}}
	candidates, err := a.Extract(context.Background(), tool, result)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Observation == "raw" {
		t.Fatalf("expected structured output, got raw")
	}
}

func TestNativeAdapter_FallbackToRaw(t *testing.T) {
	a := evidence.NewNativeAdapter(evidence.DefaultReliabilityResolver())
	tool := port.ToolDefinition{Binding: entity.ToolBinding{Source: entity.ToolSourceLocal}}
	result := &port.ToolExecutionResult{Output: "raw output", Structured: nil}
	candidates, err := a.Extract(context.Background(), tool, result)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if candidates[0].Observation != "raw output" {
		t.Fatalf("expected raw fallback, got: %s", candidates[0].Observation)
	}
}
