package evidence_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestNativeAdapter_StructuredOutput(t *testing.T) {
	a := evidence.NewNativeAdapter()
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
	a := evidence.NewNativeAdapter()
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

func TestNativeAdapter_ExtractionConfidence(t *testing.T) {
	a := evidence.NewNativeAdapter()
	tool := port.ToolDefinition{Binding: entity.ToolBinding{Source: entity.ToolSourceLocal}}
	result := &port.ToolExecutionResult{Output: "raw", Structured: map[string]any{"score": 42}}
	candidates, _ := a.Extract(context.Background(), tool, result)
	if candidates[0].Reliability.Extraction != 1.0 {
		t.Fatalf("native adapter Extraction: got %v want 1.0", candidates[0].Reliability.Extraction)
	}
	if candidates[0].Reliability.Directness != 1.0 {
		t.Fatalf("native adapter Directness for Local: got %v want 1.0", candidates[0].Reliability.Directness)
	}
}

func TestRawObservationAdapter_ExtractionConfidence(t *testing.T) {
	a := evidence.NewRawObservationAdapter()
	tool := port.ToolDefinition{Binding: entity.ToolBinding{Source: entity.ToolSourceKnowledge}}
	result := &port.ToolExecutionResult{Output: "raw output"}
	candidates, _ := a.Extract(context.Background(), tool, result)
	if candidates[0].Reliability.Extraction != 0.3 {
		t.Fatalf("raw adapter Extraction: got %v want 0.3", candidates[0].Reliability.Extraction)
	}
	if candidates[0].Reliability.Directness != 0.6 {
		t.Fatalf("raw adapter Directness for Knowledge: got %v want 0.6", candidates[0].Reliability.Directness)
	}
}
