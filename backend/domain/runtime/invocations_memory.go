package runtime

import (
	"context"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// memoryInvocationRepository is the default RuntimeInvocationRepository used
// when no durable invocation store is wired (standalone / in-process tests). It
// fences attempts within a single process but makes no cross-process promise.
type memoryInvocationRepository struct {
	mu sync.Mutex

	invocations map[string]*entity.RuntimeInvocation
	attempts    map[string][]entity.RuntimeInvocationAttempt
	current     map[string]string // invocationID -> current attemptID
}

func newMemoryInvocationRepository() *memoryInvocationRepository {
	return &memoryInvocationRepository{
		invocations: make(map[string]*entity.RuntimeInvocation),
		attempts:    make(map[string][]entity.RuntimeInvocationAttempt),
		current:     make(map[string]string),
	}
}

func (r *memoryInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.invocations[invocation.InvocationID]
	if existing == nil {
		cp := cloneRuntimeInvocation(invocation)
		r.invocations[invocation.InvocationID] = cp
		return cloneRuntimeInvocation(cp), nil
	}
	return cloneRuntimeInvocation(existing), nil
}

func (r *memoryInvocationRepository) Get(_ context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.invocations[invocationID]
	if existing == nil {
		return nil, nil
	}
	return cloneRuntimeInvocation(existing), nil
}

func (r *memoryInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv := r.invocations[invocationID]
	if inv == nil {
		return nil, false, nil
	}
	switch inv.Status {
	case entity.InvocationPending, entity.InvocationFailed, entity.InvocationUnknown:
	default:
		return cloneRuntimeInvocation(inv), false, nil
	}
	inv.Status = entity.InvocationRunning
	inv.AttemptCount++
	r.current[invocationID] = attemptID
	attempt := entity.RuntimeInvocationAttempt{
		AttemptID: attemptID, InvocationID: invocationID,
		AttemptNo: inv.AttemptCount, Status: entity.InvocationRunning,
	}
	r.attempts[invocationID] = append(r.attempts[invocationID], attempt)
	return cloneRuntimeInvocation(inv), true, nil
}

func (r *memoryInvocationRepository) Complete(_ context.Context, invocationID, attemptID, outputJSON string) (bool, error) {
	return r.finish(invocationID, attemptID, entity.InvocationSucceeded, outputJSON, "")
}

func (r *memoryInvocationRepository) Fail(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finish(invocationID, attemptID, entity.InvocationFailed, "", reason)
}

func (r *memoryInvocationRepository) MarkUnknown(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finish(invocationID, attemptID, entity.InvocationUnknown, "", reason)
}

func (r *memoryInvocationRepository) finish(invocationID, attemptID string, status entity.InvocationStatus, output, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv := r.invocations[invocationID]
	if inv == nil || inv.Status != entity.InvocationRunning || r.current[invocationID] != attemptID {
		return false, nil
	}
	inv.Status = status
	inv.OutputJSON = output
	inv.Error = reason
	return true, nil
}

func cloneRuntimeInvocation(in *entity.RuntimeInvocation) *entity.RuntimeInvocation {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

var _ port.RuntimeInvocationRepository = (*memoryInvocationRepository)(nil)
