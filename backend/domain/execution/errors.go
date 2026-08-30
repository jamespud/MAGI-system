package execution

import "errors"

var (
	// ErrAmbiguousInvocation means an external effect may have happened and the
	// runtime cannot prove its outcome.
	ErrAmbiguousInvocation = errors.New("invocation external outcome is ambiguous")

	// ErrExternalOutcomeUnknown is the executor-to-kernel ambiguity contract.
	// Executors must return or wrap it only after work may have reached an
	// external system without a definitive response. A bare context cancellation
	// is not enough because it may have happened before dispatch.
	ErrExternalOutcomeUnknown = errors.New("external outcome unknown")

	ErrInvocationNotOwned      = errors.New("invocation attempt ownership not acquired")
	ErrInvocationMismatch      = errors.New("invocation identity conflicts with persisted request")
	ErrInvocationRepository    = errors.New("invocation repository failure")
	ErrInvocationRecorder      = errors.New("invocation recorder failure")
	ErrInvalidExecutionRequest = errors.New("invalid execution request")
)
