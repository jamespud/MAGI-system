package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type RetrySafety string

const (
	RetrySafeReadOnly   RetrySafety = "read_only"
	RetrySafeIdempotent RetrySafety = "idempotent"
	RetryUnsafe         RetrySafety = "unsafe"
	RetryUnknown        RetrySafety = "unknown"
)

type Request struct {
	Identity      entity.ExecutionIdentity
	Kind          InvocationKind
	OperationName string
	Input         []byte
	RetrySafety   RetrySafety
}

type Result struct {
	Output []byte
	Cached bool
}

type ExecuteFunc func(context.Context) ([]byte, error)

// InputMatcher may explicitly approve an older input encoding after the kernel
// has verified every other immutable request field. It must fail closed.
type InputMatcher func(*entity.RuntimeInvocation, Request) (bool, error)

// InvocationRecord reports a transition only after the repository has made it
// durable. It is intentionally smaller than the model and tool runtime APIs.
type InvocationRecord struct {
	Identity entity.ExecutionIdentity
	Status   entity.InvocationStatus
	Error    string
}

type Recorder interface {
	RecordInvocation(context.Context, InvocationRecord) error
}

// Kernel fences local execution attempts and reuses durable successful output.
// It does not claim exactly-once behavior for arbitrary external systems.
type Kernel struct {
	invocations port.RuntimeInvocationRepository
	recorder    Recorder
}

func NewKernel(invocations port.RuntimeInvocationRepository, recorder Recorder) *Kernel {
	return &Kernel{invocations: invocations, recorder: recorder}
}

func (k *Kernel) Execute(ctx context.Context, req Request, fn ExecuteFunc) (*Result, error) {
	return k.execute(ctx, req, nil, fn)
}

// ExecuteWithInputMatcher permits a caller-owned, version-aware compatibility
// check for historical input encodings without relaxing normal matching.
func (k *Kernel) ExecuteWithInputMatcher(ctx context.Context, req Request, matcher InputMatcher, fn ExecuteFunc) (*Result, error) {
	return k.execute(ctx, req, matcher, fn)
}

func (k *Kernel) execute(ctx context.Context, req Request, matcher InputMatcher, fn ExecuteFunc) (*Result, error) {
	if err := k.validate(req, fn); err != nil {
		return nil, err
	}

	invocation, err := k.invocations.Ensure(ctx, &entity.RuntimeInvocation{
		InvocationID:  req.Identity.InvocationID,
		RunID:         req.Identity.RunID,
		StepID:        req.Identity.StepID,
		Kind:          string(req.Kind),
		Status:        entity.InvocationPending,
		OperationName: req.OperationName,
		RetrySafety:   string(req.RetrySafety),
		InputDigest:   digestBytes(req.Input),
		InputJSON:     string(req.Input),
	})
	if err != nil {
		return nil, repositoryError("ensure invocation", err)
	}
	if invocation == nil {
		return nil, repositoryError("ensure invocation", errors.New("repository returned nil invocation"))
	}
	if !matchesRequest(invocation, req) {
		compatible := false
		if matcher != nil && matchesRequestExceptInput(invocation, req) && invocation.InputDigest == digestBytes([]byte(invocation.InputJSON)) {
			compatible, err = matcher(invocation, req)
			if err != nil {
				return nil, fmt.Errorf("%w: invocation=%s: input compatibility: %v", ErrInvocationMismatch, req.Identity.InvocationID, err)
			}
		}
		if !compatible {
			return nil, fmt.Errorf("%w: invocation=%s", ErrInvocationMismatch, req.Identity.InvocationID)
		}
	}
	if invocation.Status == entity.InvocationSucceeded {
		return cachedResult(invocation), nil
	}
	if invocation.Status == entity.InvocationUnknown && !mayRetryUnknown(req.RetrySafety) {
		return nil, ambiguousError(req.Identity.InvocationID, nil)
	}

	invocation, won, err := k.invocations.BeginAttempt(ctx, req.Identity.InvocationID, req.Identity.AttemptID)
	if err != nil {
		return nil, repositoryError("begin attempt", err)
	}
	if !won {
		return resultWithoutFence(invocation, req)
	}

	if err := k.record(ctx, req.Identity, entity.InvocationRunning, ""); err != nil {
		recordErr := recorderError("record running attempt", err)
		won, settleErr := k.invocations.Fail(context.WithoutCancel(ctx), req.Identity.InvocationID, req.Identity.AttemptID, recordErr.Error())
		if settleErr != nil {
			return nil, errors.Join(recordErr, repositoryError("fail unrecorded attempt", settleErr))
		}
		if !won {
			return nil, errors.Join(recordErr, ownershipError(req.Identity.InvocationID, req.Identity.AttemptID))
		}
		return nil, recordErr
	}

	output, executeErr := fn(ctx)
	settleCtx := context.WithoutCancel(ctx)
	if executeErr != nil {
		if errors.Is(executeErr, ErrExternalOutcomeUnknown) {
			return nil, k.markUnknown(settleCtx, req, executeErr)
		}
		return nil, k.markFailed(settleCtx, req, executeErr)
	}

	won, err = k.invocations.Complete(settleCtx, req.Identity.InvocationID, req.Identity.AttemptID, string(output))
	if err != nil {
		return nil, repositoryError("complete attempt", err)
	}
	if !won {
		return nil, ownershipError(req.Identity.InvocationID, req.Identity.AttemptID)
	}
	if err := k.record(settleCtx, req.Identity, entity.InvocationSucceeded, ""); err != nil {
		return nil, recorderError("record succeeded attempt", err)
	}
	return &Result{Output: append([]byte(nil), output...)}, nil
}

func (k *Kernel) validate(req Request, fn ExecuteFunc) error {
	if k == nil || k.invocations == nil {
		return fmt.Errorf("%w: invocation repository is required", ErrInvalidExecutionRequest)
	}
	if fn == nil {
		return fmt.Errorf("%w: executor is required", ErrInvalidExecutionRequest)
	}
	identity := req.Identity
	if identity.RunID == "" || identity.StepID == "" || identity.InvocationID == "" || identity.AttemptID == "" {
		return fmt.Errorf("%w: complete execution identity is required", ErrInvalidExecutionRequest)
	}
	return nil
}

func (k *Kernel) markUnknown(ctx context.Context, req Request, executeErr error) error {
	ambiguousErr := ambiguousError(req.Identity.InvocationID, executeErr)
	won, err := k.invocations.MarkUnknown(ctx, req.Identity.InvocationID, req.Identity.AttemptID, executeErr.Error())
	if err != nil {
		return errors.Join(ambiguousErr, repositoryError("mark attempt unknown", err))
	}
	if !won {
		return errors.Join(ambiguousErr, ownershipError(req.Identity.InvocationID, req.Identity.AttemptID))
	}
	if err := k.record(ctx, req.Identity, entity.InvocationUnknown, executeErr.Error()); err != nil {
		return errors.Join(ambiguousErr, recorderError("record unknown attempt", err))
	}
	return ambiguousErr
}

func (k *Kernel) markFailed(ctx context.Context, req Request, executeErr error) error {
	won, err := k.invocations.Fail(ctx, req.Identity.InvocationID, req.Identity.AttemptID, executeErr.Error())
	if err != nil {
		return errors.Join(executeErr, repositoryError("fail attempt", err))
	}
	if !won {
		return errors.Join(executeErr, ownershipError(req.Identity.InvocationID, req.Identity.AttemptID))
	}
	if err := k.record(ctx, req.Identity, entity.InvocationFailed, executeErr.Error()); err != nil {
		return errors.Join(executeErr, recorderError("record failed attempt", err))
	}
	return executeErr
}

func (k *Kernel) record(ctx context.Context, identity entity.ExecutionIdentity, status entity.InvocationStatus, reason string) error {
	if k.recorder == nil {
		return nil
	}
	return k.recorder.RecordInvocation(ctx, InvocationRecord{Identity: identity, Status: status, Error: reason})
}

func resultWithoutFence(invocation *entity.RuntimeInvocation, req Request) (*Result, error) {
	if invocation != nil && invocation.Status == entity.InvocationSucceeded {
		return cachedResult(invocation), nil
	}
	if invocation != nil && invocation.Status == entity.InvocationUnknown && !mayRetryUnknown(req.RetrySafety) {
		return nil, ambiguousError(req.Identity.InvocationID, nil)
	}
	return nil, ownershipError(req.Identity.InvocationID, req.Identity.AttemptID)
}

func cachedResult(invocation *entity.RuntimeInvocation) *Result {
	return &Result{Output: []byte(invocation.OutputJSON), Cached: true}
}

func mayRetryUnknown(safety RetrySafety) bool {
	return safety == RetrySafeReadOnly || safety == RetrySafeIdempotent
}

func matchesRequest(invocation *entity.RuntimeInvocation, req Request) bool {
	return matchesRequestExceptInput(invocation, req) &&
		invocation.InputDigest == digestBytes(req.Input) &&
		invocation.InputJSON == string(req.Input)
}

func matchesRequestExceptInput(invocation *entity.RuntimeInvocation, req Request) bool {
	return invocation.InvocationID == req.Identity.InvocationID &&
		invocation.RunID == req.Identity.RunID &&
		invocation.StepID == req.Identity.StepID &&
		invocation.Kind == string(req.Kind) &&
		invocation.OperationName == req.OperationName &&
		invocation.RetrySafety == string(req.RetrySafety)
}

func ambiguousError(invocationID string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: invocation %s", ErrAmbiguousInvocation, invocationID)
	}
	return fmt.Errorf("%w: invocation %s: %w", ErrAmbiguousInvocation, invocationID, cause)
}

func repositoryError(operation string, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrInvocationRepository, operation, cause)
}

func recorderError(operation string, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrInvocationRecorder, operation, cause)
}

func ownershipError(invocationID, attemptID string) error {
	return fmt.Errorf("%w: invocation=%s attempt=%s", ErrInvocationNotOwned, invocationID, attemptID)
}
