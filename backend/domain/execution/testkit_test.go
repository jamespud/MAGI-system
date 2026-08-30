package execution

import (
	"context"
	"errors"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
)

type memoryInvocationRepository struct {
	mu sync.Mutex

	invocation       *entity.RuntimeInvocation
	currentAttemptID string
	attempts         []entity.RuntimeInvocationAttempt

	ensureErr error
	beginErr  error
	finishErr error
	loseBegin bool
}

func (r *memoryInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ensureErr != nil {
		return nil, r.ensureErr
	}
	if r.invocation == nil {
		r.invocation = cloneRuntimeInvocation(invocation)
	}
	return cloneRuntimeInvocation(r.invocation), nil
}

func (r *memoryInvocationRepository) Get(_ context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil || r.invocation.InvocationID != invocationID {
		return nil, errors.New("invocation not found")
	}
	return cloneRuntimeInvocation(r.invocation), nil
}

func (r *memoryInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beginErr != nil {
		return nil, false, r.beginErr
	}
	if r.invocation == nil || r.invocation.InvocationID != invocationID {
		return nil, false, errors.New("invocation not found")
	}
	if r.loseBegin {
		return cloneRuntimeInvocation(r.invocation), false, nil
	}
	switch r.invocation.Status {
	case entity.InvocationPending, entity.InvocationFailed, entity.InvocationUnknown:
	default:
		return cloneRuntimeInvocation(r.invocation), false, nil
	}
	r.invocation.Status = entity.InvocationRunning
	r.invocation.AttemptCount++
	r.currentAttemptID = attemptID
	r.attempts = append(r.attempts, entity.RuntimeInvocationAttempt{
		AttemptID: attemptID, InvocationID: invocationID,
		AttemptNo: r.invocation.AttemptCount, Status: entity.InvocationRunning,
	})
	return cloneRuntimeInvocation(r.invocation), true, nil
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
	if r.finishErr != nil {
		return false, r.finishErr
	}
	if r.invocation == nil || r.invocation.InvocationID != invocationID || r.invocation.Status != entity.InvocationRunning || r.currentAttemptID != attemptID {
		return false, nil
	}
	r.invocation.Status = status
	r.invocation.OutputJSON = output
	r.invocation.Error = reason
	for i := range r.attempts {
		if r.attempts[i].AttemptID == attemptID {
			r.attempts[i].Status = status
			r.attempts[i].Error = reason
		}
	}
	return true, nil
}

func (r *memoryInvocationRepository) snapshot() (*entity.RuntimeInvocation, []entity.RuntimeInvocationAttempt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	attempts := append([]entity.RuntimeInvocationAttempt(nil), r.attempts...)
	return cloneRuntimeInvocation(r.invocation), attempts
}

func cloneRuntimeInvocation(invocation *entity.RuntimeInvocation) *entity.RuntimeInvocation {
	if invocation == nil {
		return nil
	}
	clone := *invocation
	return &clone
}

type recordingRecorder struct {
	mu         sync.Mutex
	records    []InvocationRecord
	failStatus entity.InvocationStatus
	err        error
}

func (r *recordingRecorder) RecordInvocation(_ context.Context, record InvocationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.Status == r.failStatus && r.err != nil {
		return r.err
	}
	r.records = append(r.records, record)
	return nil
}

func (r *recordingRecorder) snapshot() []InvocationRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]InvocationRecord(nil), r.records...)
}
