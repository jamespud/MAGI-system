package toolruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/validation"
)

// An executor error reaches magi_tool_call.err, the TOOL_CALL_FAILED event
// payload, and the model's next message without ever being validated. A failing
// external tool could therefore replay the MySQL 1406 overflow the output clamp
// was added to stop, so the same bound has to cover the error path.
func TestToolRuntime_BoundsExecutorErrorText(t *testing.T) {
	blob := strings.Repeat("x", defaultToolErrorMaxBytes*4)
	sentinel := errors.New("upstream rejected the call")
	reg := metrics.New()
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{
		err: fmt.Errorf("mcp server %q tool %q failed: %s: %w", "evil", "echo", blob, sentinel),
	}
	runtime, err := New(Deps{
		Kernel:    execution.NewKernel(repo, nil),
		Executor:  executor,
		Validator: validation.NewJSONSchemaValidator(),
		Metrics:   reg,
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	_, err = runtime.Execute(context.Background(), testRequest("attempt-1"))
	if err == nil {
		t.Fatal("execute returned nil error, want the executor failure")
	}
	const slack = 128 // truncation marker + redaction headroom
	if len(err.Error()) > defaultToolErrorMaxBytes+slack {
		t.Fatalf("error text = %d bytes, want <= %d", len(err.Error()), defaultToolErrorMaxBytes+slack)
	}
	if !strings.Contains(err.Error(), "output truncated") {
		t.Fatalf("error text is missing the truncation marker: %q", err.Error())
	}
	// Shortening the message must not break classification: callers branch on
	// the wrapped error with errors.Is.
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want it to keep wrapping %v", err, sentinel)
	}
	if got := reg.ToolErrorClipped.Load(); got != 1 {
		t.Fatalf("magi_tool_error_clipped_total = %d, want 1", got)
	}
}

func TestBoundErrorMessage_LeavesSmallErrorsAlone(t *testing.T) {
	err := errors.New("boom")
	got, changed := boundErrorMessage(err, defaultToolErrorMaxBytes)
	if changed {
		t.Fatal("small error was reported as clipped")
	}
	if got != err {
		t.Fatalf("error = %v, want the original value", got)
	}
}

func TestBoundErrorMessage_CutsOnRuneBoundary(t *testing.T) {
	// Every rune is 3 bytes, so a naive byte cut splits one. The text that
	// reaches MySQL has to stay valid UTF-8.
	err := fmt.Errorf("%s", strings.Repeat("漢", 4000))
	got, changed := boundErrorMessage(err, 10)
	if !changed {
		t.Fatal("oversized error was not clipped")
	}
	if !utf8.ValidString(got.Error()) {
		t.Fatalf("clipped error is not valid UTF-8: %q", got.Error())
	}
	if len(got.Error()) > 10+128 {
		t.Fatalf("clipped error = %d bytes, want <= %d", len(got.Error()), 10+128)
	}
}
