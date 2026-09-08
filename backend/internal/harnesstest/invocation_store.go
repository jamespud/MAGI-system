package harnesstest

import (
	"context"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// RecordingInvocationRepository is an in-memory
// port.RuntimeInvocationRepository that also records each transition for crash
// recovery assertions.
type RecordingInvocationRepository struct {
	mu sync.Mutex

	invocations map[string]*entity.RuntimeInvocation
	attempts    map[string][]entity.RuntimeInvocationAttempt
	current     map[string]string

	EnsureCalls       int
	GetCalls          int
	BeginAttemptCalls int
	CompleteCalls     int
	FailCalls         int
	MarkUnknownCalls  int
}

func NewRecordingInvocationRepository() *RecordingInvocationRepository {
	return &RecordingInvocationRepository{
		invocations: make(map[string]*entity.RuntimeInvocation),
		attempts:    make(map[string][]entity.RuntimeInvocationAttempt),
		current:     make(map[string]string),
	}
}

func (r *RecordingInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.EnsureCalls++
	existing := r.invocations[invocation.InvocationID]
	if existing == nil {
		cp := *invocation
		r.invocations[invocation.InvocationID] = &cp
		existing = &cp
	}
	clone := *existing
	return &clone, nil
}

func (r *RecordingInvocationRepository) Get(_ context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.GetCalls++
	existing := r.invocations[invocationID]
	if existing == nil {
		return nil, nil
	}
	clone := *existing
	return &clone, nil
}

func (r *RecordingInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BeginAttemptCalls++
	inv := r.invocations[invocationID]
	if inv == nil {
		return nil, false, nil
	}
	switch inv.Status {
	case entity.InvocationPending, entity.InvocationFailed, entity.InvocationUnknown:
	default:
		clone := *inv
		return &clone, false, nil
	}
	inv.Status = entity.InvocationRunning
	inv.AttemptCount++
	r.current[invocationID] = attemptID
	r.attempts[invocationID] = append(r.attempts[invocationID], entity.RuntimeInvocationAttempt{
		AttemptID: attemptID, InvocationID: invocationID, AttemptNo: inv.AttemptCount, Status: entity.InvocationRunning,
	})
	clone := *inv
	return &clone, true, nil
}

func (r *RecordingInvocationRepository) Complete(_ context.Context, invocationID, attemptID, outputJSON string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.CompleteCalls++
	return r.finishLocked(invocationID, attemptID, entity.InvocationSucceeded, outputJSON, ""), nil
}

func (r *RecordingInvocationRepository) Fail(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FailCalls++
	return r.finishLocked(invocationID, attemptID, entity.InvocationFailed, "", reason), nil
}

func (r *RecordingInvocationRepository) MarkUnknown(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.MarkUnknownCalls++
	return r.finishLocked(invocationID, attemptID, entity.InvocationUnknown, "", reason), nil
}

func (r *RecordingInvocationRepository) finishLocked(invocationID, attemptID string, status entity.InvocationStatus, output, reason string) bool {
	inv := r.invocations[invocationID]
	if inv == nil || inv.Status != entity.InvocationRunning || r.current[invocationID] != attemptID {
		return false
	}
	inv.Status = status
	inv.OutputJSON = output
	inv.Error = reason
	return true
}

func (r *RecordingInvocationRepository) Invocation(id string) *entity.RuntimeInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inv := r.invocations[id]; inv != nil {
		clone := *inv
		return &clone
	}
	return nil
}

var _ port.RuntimeInvocationRepository = (*RecordingInvocationRepository)(nil)
