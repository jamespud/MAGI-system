package toolruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

func TestToolRuntime_ClipsOversizedOutput(t *testing.T) {
	blob := strings.Repeat("x", defaultToolOutputMaxBytes*2)
	reg := metrics.New()
	repo := &memoryInvocationRepository{}
	runtime, err := New(Deps{
		Kernel:    execution.NewKernel(repo, nil),
		Executor:  &recordingExecutor{result: &port.ToolExecutionResult{Output: blob, Structured: map[string]any{"blob": blob}}},
		Validator: validation.NewJSONSchemaValidator(),
		Metrics:   reg,
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	res, err := runtime.Execute(context.Background(), testRequest("attempt-1"))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	const slack = 128 // truncation marker + redaction headroom
	if len(res.Output) > defaultToolOutputMaxBytes+slack {
		t.Fatalf("returned output = %d bytes, want <= %d", len(res.Output), defaultToolOutputMaxBytes+slack)
	}
	if !strings.Contains(res.Output, "output truncated") {
		t.Fatalf("returned output is missing the truncation marker: %q", res.Output[:64])
	}
	if res.Execution == nil || len(res.Execution.Output) > defaultToolOutputMaxBytes+slack {
		t.Fatalf("persisted execution output unbounded: %#v", res.Execution)
	}
	if got := reg.ToolOutputClipped.Load(); got != 1 {
		t.Fatalf("ToolOutputClipped = %d, want 1", got)
	}
}

func TestClipToolOutput_KeepsSmallOutputIntact(t *testing.T) {
	in := &port.ToolExecutionResult{Output: "small"}
	if clipToolOutput(in, defaultToolOutputMaxBytes) {
		t.Fatal("small output reported as truncated")
	}
	if in.Output != "small" {
		t.Fatalf("output mutated: %q", in.Output)
	}
}

func TestClipToolOutput_CutsOnRuneBoundary(t *testing.T) {
	// "中" is 3 bytes; a cut in the middle of it must back off.
	in := &port.ToolExecutionResult{Output: strings.Repeat("中", 8)}
	if !clipToolOutput(in, 7) {
		t.Fatal("expected truncation")
	}
	for i, r := range in.Output {
		if r == '\uFFFD' {
			t.Fatalf("invalid rune at byte %d: %q", i, in.Output)
		}
	}
}
