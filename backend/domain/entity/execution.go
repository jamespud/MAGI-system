package entity

// ExecutionIdentity distinguishes a logical invocation from its physical
// execution attempt.
type ExecutionIdentity struct {
	RunID        string
	StepID       string
	InvocationID string
	AttemptID    string
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
