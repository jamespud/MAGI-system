package entity

import (
	"time"
)

// ExecutionIdentity distinguishes a logical invocation from its physical
// execution attempt.
type ExecutionIdentity struct {
	RunID        string
	StepID       string
	InvocationID string
	AttemptID    string
}

// ExecutionContext is the explicit durable owner identity carried by one
// execution. JobAttempt and RunID are diagnostic/runtime identities; fencing
// uses CaseID + JobID + WorkerID + ExecutionGeneration and the durable lease.
type ExecutionContext struct {
	CaseID              string
	JobID               string
	RunID               string
	WorkerID            string
	JobAttempt          int
	ExecutionGeneration int64
}

func (e *ExecutionContext) IsDurable() bool {
	return e != nil && e.CaseID != "" && e.JobID != "" && e.WorkerID != "" && e.ExecutionGeneration > 0
}

type InvocationStatus string

const (
	InvocationPending   InvocationStatus = "pending"
	InvocationRunning   InvocationStatus = "running"
	InvocationSucceeded InvocationStatus = "succeeded"
	InvocationFailed    InvocationStatus = "failed"

	// External outcome may have happened but cannot be proven.
	InvocationUnknown InvocationStatus = "unknown"
)

// RuntimeInvocation is the durable logical operation. Its identity is stable
// across physical attempts, which are recorded separately.
type RuntimeInvocation struct {
	InvocationID   string
	RunID          string
	StepID         string
	Kind           string
	LogicalOrdinal int
	Status         InvocationStatus
	AttemptCount   int
	OperationName  string
	RetrySafety    string
	IdempotencyKey *string
	InputDigest    string
	InputJSON      string
	OutputJSON     string
	Error          string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	UpdatedAt      time.Time
}

// RuntimeInvocationAttempt records one physical execution of a logical
// invocation. AttemptID is deliberately not the logical primary key.
type RuntimeInvocationAttempt struct {
	AttemptID    string
	InvocationID string
	AttemptNo    int
	WorkerID     string
	Status       InvocationStatus
	StartedAt    *time.Time
	CompletedAt  *time.Time
	Error        string
}
