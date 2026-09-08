package execution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestKernelCompletedInvocationIsNotExecutedAgain(t *testing.T) {
	repo := &memoryInvocationRepository{}
	recorder := &recordingRecorder{}
	kernel := NewKernel(repo, recorder)
	calls := 0
	executor := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`{"answer":42}`), nil
	}

	first, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetryUnsafe), executor)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := kernel.Execute(context.Background(), kernelRequest("attempt-2", RetryUnsafe), executor)
	if err != nil {
		t.Fatalf("cached execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
	if first.Cached || !second.Cached || string(second.Output) != `{"answer":42}` {
		t.Fatalf("results = first=%+v second=%+v, want fresh then cached output", first, second)
	}
	_, attempts := repo.snapshot()
	if len(attempts) != 1 {
		t.Fatalf("attempts = %+v, want exactly one physical attempt", attempts)
	}
}

func TestKernelPersistsIdempotencyKeyAndOrdinal(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	req := Request{
		Identity:       entity.ExecutionIdentity{RunID: "run-idem", StepID: "step-idem", InvocationID: "inv-idem", AttemptID: "att-1"},
		Kind:           InvocationTool,
		OperationName:  "calc",
		Input:          []byte(`{"a":1}`),
		RetrySafety:    RetrySafeIdempotent,
		IdempotencyKey: "idem-key-1",
		LogicalOrdinal: 3,
	}
	calls := 0
	if _, err := kernel.Execute(context.Background(), req, func(context.Context) ([]byte, error) {
		calls++
		return []byte("ok"), nil
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	stored, err := repo.Get(context.Background(), "inv-idem")
	if err != nil {
		t.Fatalf("get stored: %v", err)
	}
	if stored.IdempotencyKey == nil || *stored.IdempotencyKey != "idem-key-1" {
		t.Fatalf("persisted idempotency key = %v, want idem-key-1", stored.IdempotencyKey)
	}
	if stored.LogicalOrdinal != 3 {
		t.Fatalf("persisted logical ordinal = %d, want 3", stored.LogicalOrdinal)
	}

	// Replay with the same idempotency key is served from cache and does not
	// create a second attempt.
	retry := req
	retry.Identity.AttemptID = "att-2"
	result, err := kernel.Execute(context.Background(), retry, func(context.Context) ([]byte, error) {
		calls++
		return []byte("must not rerun"), nil
	})
	if err != nil {
		t.Fatalf("cached execute: %v", err)
	}
	if !result.Cached || calls != 1 {
		t.Fatalf("cached=%v calls=%d, want cached after exactly one execution", result.Cached, calls)
	}
}

func TestKernelRejectsIdempotencyKeyMismatch(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	req := Request{
		Identity:       entity.ExecutionIdentity{RunID: "run-idem", StepID: "step-idem", InvocationID: "inv-idem", AttemptID: "att-1"},
		Kind:           InvocationTool,
		OperationName:  "calc",
		Input:          []byte(`{"a":1}`),
		RetrySafety:    RetrySafeIdempotent,
		IdempotencyKey: "idem-key-1",
	}
	if _, err := kernel.Execute(context.Background(), req, func(context.Context) ([]byte, error) {
		return []byte("ok"), nil
	}); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	changed := req
	changed.Identity.AttemptID = "att-2"
	changed.IdempotencyKey = "idem-key-DIFFERENT"
	if _, err := kernel.Execute(context.Background(), changed, func(context.Context) ([]byte, error) {
		return []byte("must not run"), nil
	}); !errors.Is(err, ErrInvocationMismatch) {
		t.Fatalf("second execute error = %v, want ErrInvocationMismatch", err)
	}
}

func TestKernelInvocationIdentityMismatchPreventsExecution(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	calls := 0
	firstRequest := kernelRequest("attempt-1", RetrySafeReadOnly)
	if _, err := kernel.Execute(context.Background(), firstRequest, func(context.Context) ([]byte, error) {
		calls++
		return []byte("first"), nil
	}); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	changedRequest := kernelRequest("attempt-2", RetrySafeReadOnly)
	changedRequest.Input = []byte(`{"prompt":"different"}`)
	_, err := kernel.Execute(context.Background(), changedRequest, func(context.Context) ([]byte, error) {
		calls++
		return []byte("must not run"), nil
	})
	if !errors.Is(err, ErrInvocationMismatch) {
		t.Fatalf("changed execute error = %v, want ErrInvocationMismatch", err)
	}
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
}

func TestKernelRetryUsesNewAttemptButSameInvocation(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	knownFailure := errors.New("provider rejected request")
	calls := 0

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetryUnsafe), func(context.Context) ([]byte, error) {
		calls++
		return nil, knownFailure
	})
	if !errors.Is(err, knownFailure) {
		t.Fatalf("first execute error = %v, want known failure", err)
	}

	result, err := kernel.Execute(context.Background(), kernelRequest("attempt-2", RetryUnsafe), func(context.Context) ([]byte, error) {
		calls++
		return []byte("ok"), nil
	})
	if err != nil {
		t.Fatalf("retry execute: %v", err)
	}
	if calls != 2 || string(result.Output) != "ok" {
		t.Fatalf("calls=%d result=%+v, want two calls and successful retry", calls, result)
	}
	invocation, attempts := repo.snapshot()
	if invocation.InvocationID != "invocation-1" || len(attempts) != 2 {
		t.Fatalf("invocation=%+v attempts=%+v, want one logical invocation and two attempts", invocation, attempts)
	}
	if attempts[0].InvocationID != attempts[1].InvocationID || attempts[0].AttemptID != "attempt-1" || attempts[1].AttemptID != "attempt-2" {
		t.Fatalf("attempt identities = %+v, want stable invocation and distinct attempts", attempts)
	}
}

func TestKernelUnsafeUnknownInvocationDoesNotRetry(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	calls := 0
	ambiguousExecutor := func(context.Context) ([]byte, error) {
		calls++
		return nil, fmt.Errorf("connection lost after dispatch: %w", ErrExternalOutcomeUnknown)
	}

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetryUnsafe), ambiguousExecutor)
	if !errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("first execute error = %v, want ErrAmbiguousInvocation", err)
	}
	_, err = kernel.Execute(context.Background(), kernelRequest("attempt-2", RetryUnsafe), ambiguousExecutor)
	if !errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("retry execute error = %v, want ErrAmbiguousInvocation", err)
	}
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
	invocation, attempts := repo.snapshot()
	if invocation.Status != entity.InvocationUnknown || len(attempts) != 1 {
		t.Fatalf("invocation=%+v attempts=%+v, want one unknown attempt", invocation, attempts)
	}
}

func TestKernelUnsafeUnknownCannotBeReclassifiedForRetry(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	calls := 0

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetryUnsafe), func(context.Context) ([]byte, error) {
		calls++
		return nil, ErrExternalOutcomeUnknown
	})
	if !errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("unsafe execute error = %v, want ErrAmbiguousInvocation", err)
	}

	_, err = kernel.Execute(context.Background(), kernelRequest("attempt-2", RetrySafeIdempotent), func(context.Context) ([]byte, error) {
		calls++
		return []byte("must not run"), nil
	})
	if !errors.Is(err, ErrInvocationMismatch) {
		t.Fatalf("reclassified retry error = %v, want ErrInvocationMismatch", err)
	}
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
	invocation, attempts := repo.snapshot()
	if invocation.Status != entity.InvocationUnknown || len(attempts) != 1 {
		t.Fatalf("invocation=%+v attempts=%+v, want one unknown unsafe attempt", invocation, attempts)
	}
}

func TestKernelIdempotentInvocationMayRetry(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)
	calls := 0

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetrySafeIdempotent), func(context.Context) ([]byte, error) {
		calls++
		return nil, ErrExternalOutcomeUnknown
	})
	if !errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("first execute error = %v, want ErrAmbiguousInvocation", err)
	}
	result, err := kernel.Execute(context.Background(), kernelRequest("attempt-2", RetrySafeIdempotent), func(context.Context) ([]byte, error) {
		calls++
		return []byte("recovered"), nil
	})
	if err != nil {
		t.Fatalf("retry execute: %v", err)
	}
	if calls != 2 || string(result.Output) != "recovered" {
		t.Fatalf("calls=%d result=%+v, want retry success", calls, result)
	}
	invocation, attempts := repo.snapshot()
	if invocation.Status != entity.InvocationSucceeded || len(attempts) != 2 || attempts[0].AttemptID == attempts[1].AttemptID {
		t.Fatalf("invocation=%+v attempts=%+v, want succeeded with distinct attempts", invocation, attempts)
	}
}

func TestKernelRepositoryFailurePreventsExecution(t *testing.T) {
	repositoryFailure := errors.New("repository unavailable")
	for _, test := range []struct {
		name string
		repo *memoryInvocationRepository
	}{
		{name: "ensure", repo: &memoryInvocationRepository{ensureErr: repositoryFailure}},
		{name: "begin", repo: &memoryInvocationRepository{beginErr: repositoryFailure}},
	} {
		t.Run(test.name, func(t *testing.T) {
			kernel := NewKernel(test.repo, nil)
			calls := 0
			_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetrySafeReadOnly), func(context.Context) ([]byte, error) {
				calls++
				return []byte("must not run"), nil
			})
			if !errors.Is(err, ErrInvocationRepository) {
				t.Fatalf("execute error = %v, want ErrInvocationRepository", err)
			}
			if calls != 0 {
				t.Fatalf("executor calls = %d, want 0", calls)
			}
		})
	}
}

func TestKernelLostAttemptFenceDoesNotExecute(t *testing.T) {
	repo := &memoryInvocationRepository{loseBegin: true}
	kernel := NewKernel(repo, nil)
	calls := 0

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetrySafeReadOnly), func(context.Context) ([]byte, error) {
		calls++
		return []byte("must not run"), nil
	})
	if !errors.Is(err, ErrInvocationNotOwned) {
		t.Fatalf("execute error = %v, want ErrInvocationNotOwned", err)
	}
	if calls != 0 {
		t.Fatalf("executor calls = %d, want 0", calls)
	}
}

func TestKernelBareCancellationIsKnownFailure(t *testing.T) {
	repo := &memoryInvocationRepository{}
	kernel := NewKernel(repo, nil)

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetryUnsafe), func(context.Context) ([]byte, error) {
		return nil, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("execute error = %v, want bare cancellation as known failure", err)
	}
	invocation, _ := repo.snapshot()
	if invocation.Status != entity.InvocationFailed {
		t.Fatalf("invocation status = %q, want failed", invocation.Status)
	}
}

func TestKernelRecorderTracksDurableTransitions(t *testing.T) {
	repo := &memoryInvocationRepository{}
	recorder := &recordingRecorder{}
	kernel := NewKernel(repo, recorder)

	if _, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetrySafeReadOnly), func(context.Context) ([]byte, error) {
		return []byte("ok"), nil
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	records := recorder.snapshot()
	if len(records) != 2 || records[0].Status != entity.InvocationRunning || records[1].Status != entity.InvocationSucceeded {
		t.Fatalf("records = %+v, want running then succeeded", records)
	}
	for _, record := range records {
		if record.Identity.InvocationID != "invocation-1" || record.Identity.AttemptID != "attempt-1" {
			t.Fatalf("record identity = %+v, want invocation-1/attempt-1", record.Identity)
		}
	}
}

func TestKernelRecorderFailureBeforeExecutionPreventsExecution(t *testing.T) {
	repo := &memoryInvocationRepository{}
	recordFailure := errors.New("critical recorder unavailable")
	recorder := &recordingRecorder{failStatus: entity.InvocationRunning, err: recordFailure}
	kernel := NewKernel(repo, recorder)
	calls := 0

	_, err := kernel.Execute(context.Background(), kernelRequest("attempt-1", RetrySafeReadOnly), func(context.Context) ([]byte, error) {
		calls++
		return []byte("must not run"), nil
	})
	if !errors.Is(err, ErrInvocationRecorder) || !errors.Is(err, recordFailure) {
		t.Fatalf("execute error = %v, want recorder failure", err)
	}
	if calls != 0 {
		t.Fatalf("executor calls = %d, want 0", calls)
	}
	invocation, _ := repo.snapshot()
	if invocation.Status != entity.InvocationFailed {
		t.Fatalf("invocation status = %q, want failed", invocation.Status)
	}
}

func kernelRequest(attemptID string, safety RetrySafety) Request {
	return Request{
		Identity: entity.ExecutionIdentity{
			RunID: "run-1", StepID: "step-1", InvocationID: "invocation-1", AttemptID: attemptID,
		},
		Kind: InvocationModel, OperationName: "generate", Input: []byte(`{"prompt":"hello"}`), RetrySafety: safety,
	}
}
