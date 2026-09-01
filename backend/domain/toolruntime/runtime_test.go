package toolruntime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

func TestToolRuntimeDeniedToolNeverReachesKernel(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{}
	runtime := newRuntime(t, repo, executor, nil)

	_, err := runtime.Execute(context.Background(), Request{
		Identity:      testIdentity("attempt-1"),
		Definition:    port.ToolDefinition{Name: "restricted"},
		ArgumentsJSON: `{"value":1}`,
		Permission: func(context.Context, port.ToolDefinition) error {
			return errors.New("tool is not permitted")
		},
	})
	if !errors.Is(err, ErrToolDenied) {
		t.Fatalf("execute error = %v, want denied", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if repo.invocation != nil {
		t.Fatalf("kernel invocation = %+v, want nil", repo.invocation)
	}
}

func TestToolRuntimeQuotaFailureNeverExecutes(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{}
	runtime := newRuntime(t, repo, executor, &fakeQuota{allowed: false})

	_, err := runtime.Execute(context.Background(), testRequest("attempt-1"))
	if !errors.Is(err, ErrToolQuotaExceeded) {
		t.Fatalf("execute error = %v, want quota exceeded", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if repo.invocation != nil {
		t.Fatalf("kernel invocation = %+v, want nil", repo.invocation)
	}
}

func TestToolRuntimeValidationFailureNeverExecutes(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{}
	runtime := newRuntime(t, repo, executor, nil)
	req := testRequest("attempt-1")
	req.ArgumentsJSON = `{"value":"not-an-integer"}`

	result, err := runtime.Execute(context.Background(), req)
	if !errors.Is(err, ErrToolArgumentsInvalid) {
		t.Fatalf("execute error = %v, want invalid arguments", err)
	}
	if result == nil || len(result.Violations) == 0 {
		t.Fatalf("validation result = %+v, want violations", result)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if repo.invocation != nil {
		t.Fatalf("kernel invocation = %+v, want nil", repo.invocation)
	}
}

func TestToolRuntimeCompletedInvocationReturnsCachedResult(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{result: &port.ToolExecutionResult{
		Output: "first", Structured: map[string]any{"score": 1}, SourceURI: "https://source.example/result",
	}}
	runtime := newRuntime(t, repo, executor, nil)

	first, err := runtime.Execute(context.Background(), testRequest("attempt-1"))
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := runtime.Execute(context.Background(), testRequest("attempt-2"))
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if first.Cached {
		t.Fatal("first execution unexpectedly cached")
	}
	if !second.Cached {
		t.Fatal("second execution was not cached")
	}
	if second.Execution == nil || second.Execution.Output != "first" {
		t.Fatalf("cached result = %+v, want output first", second.Execution)
	}
	if second.Execution.SourceURI != "https://source.example/result" {
		t.Fatalf("cached source URI = %q", second.Execution.SourceURI)
	}
	structured, ok := second.Execution.Structured.(map[string]any)
	if !ok || structured["score"] != float64(1) {
		t.Fatalf("cached structured result = %#v", second.Execution.Structured)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
}

func TestToolRuntimePassesStableIdempotencyKey(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{result: &port.ToolExecutionResult{Output: "ok"}}
	runtime := newRuntime(t, repo, executor, nil)
	req := testRequest("attempt-1")
	req.ArgumentsJSON = `{"b":2,"a":1}`

	result, err := runtime.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("executor requests = %d, want 1", len(executor.requests))
	}
	want := execution.ToolIdempotencyKey(req.Identity.InvocationID, req.Definition.Name, []byte(`{"a":1,"b":2}`))
	if result.IdempotencyKey != want {
		t.Fatalf("result idempotency key = %q, want %q", result.IdempotencyKey, want)
	}
	if executor.requests[0].IdempotencyKey != want {
		t.Fatalf("executor idempotency key = %q, want %q", executor.requests[0].IdempotencyKey, want)
	}
	if executor.requests[0].AttemptID != req.Identity.AttemptID {
		t.Fatalf("executor attempt ID = %q, want %q", executor.requests[0].AttemptID, req.Identity.AttemptID)
	}
}

func TestToolRuntimeUnsafeAmbiguousResultRequiresRecovery(t *testing.T) {
	repo := &memoryInvocationRepository{}
	executor := &recordingExecutor{err: execution.ErrExternalOutcomeUnknown}
	runtime := newRuntime(t, repo, executor, nil)

	_, err := runtime.Execute(context.Background(), testRequest("attempt-1"))
	if !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("first execute error = %v, want ambiguous invocation", err)
	}
	_, err = runtime.Execute(context.Background(), testRequest("attempt-2"))
	if !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("retry error = %v, want recovery-required ambiguous invocation", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1 for zero-value unknown effect class", executor.calls)
	}
}

func newRuntime(t *testing.T, repo *memoryInvocationRepository, executor *recordingExecutor, quota port.ToolQuotaPort) *Runtime {
	t.Helper()
	runtime, err := New(Deps{
		Kernel:    execution.NewKernel(repo, nil),
		Executor:  executor,
		Validator: validation.NewJSONSchemaValidator(),
		Quota:     quota,
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	return runtime
}

func testRequest(attemptID string) Request {
	return Request{
		Identity: entity.ExecutionIdentity{
			RunID: "run-1", StepID: "step-1", InvocationID: "tool-invocation-1", AttemptID: attemptID,
		},
		Definition: port.ToolDefinition{
			Name:       "calculator",
			ArgsSchema: []byte(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"},"value":{"type":"integer"}},"required":["a"],"additionalProperties":false}`),
			Binding:    entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calculator"},
		},
		ArgumentsJSON: `{"a":1}`,
		UserID:        "user-1",
	}
}

func testIdentity(attemptID string) entity.ExecutionIdentity {
	return entity.ExecutionIdentity{RunID: "run-1", StepID: "step-1", InvocationID: "tool-invocation-1", AttemptID: attemptID}
}

type fakeQuota struct {
	allowed bool
	calls   int
}

func (q *fakeQuota) Allow(context.Context, string, string) (bool, error) {
	q.calls++
	return q.allowed, nil
}

type recordingExecutor struct {
	calls    int
	requests []port.ToolExecutionRequest
	result   *port.ToolExecutionResult
	err      error
}

func (e *recordingExecutor) Execute(_ context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	e.calls++
	e.requests = append(e.requests, req)
	if e.err != nil {
		return nil, e.err
	}
	if e.result == nil {
		return &port.ToolExecutionResult{Output: "ok"}, nil
	}
	return e.result, nil
}

type memoryInvocationRepository struct {
	mu sync.Mutex

	invocation       *entity.RuntimeInvocation
	currentAttemptID string
}

func (r *memoryInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil {
		clone := *invocation
		r.invocation = &clone
	}
	clone := *r.invocation
	return &clone, nil
}

func (r *memoryInvocationRepository) Get(_ context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil || r.invocation.InvocationID != invocationID {
		return nil, errors.New("invocation not found")
	}
	clone := *r.invocation
	return &clone, nil
}

func (r *memoryInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil || r.invocation.InvocationID != invocationID {
		return nil, false, errors.New("invocation not found")
	}
	if r.invocation.Status != entity.InvocationPending && r.invocation.Status != entity.InvocationFailed && r.invocation.Status != entity.InvocationUnknown {
		clone := *r.invocation
		return &clone, false, nil
	}
	r.invocation.Status = entity.InvocationRunning
	r.currentAttemptID = attemptID
	clone := *r.invocation
	return &clone, true, nil
}

func (r *memoryInvocationRepository) Complete(_ context.Context, invocationID, attemptID, output string) (bool, error) {
	return r.settle(invocationID, attemptID, entity.InvocationSucceeded, output)
}

func (r *memoryInvocationRepository) Fail(_ context.Context, invocationID, attemptID, _ string) (bool, error) {
	return r.settle(invocationID, attemptID, entity.InvocationFailed, "")
}

func (r *memoryInvocationRepository) MarkUnknown(_ context.Context, invocationID, attemptID, _ string) (bool, error) {
	return r.settle(invocationID, attemptID, entity.InvocationUnknown, "")
}

func (r *memoryInvocationRepository) settle(invocationID, attemptID string, status entity.InvocationStatus, output string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil || r.invocation.InvocationID != invocationID || r.invocation.Status != entity.InvocationRunning || r.currentAttemptID != attemptID {
		return false, nil
	}
	r.invocation.Status = status
	r.invocation.OutputJSON = output
	return true, nil
}
