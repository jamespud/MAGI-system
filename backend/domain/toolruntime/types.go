package toolruntime

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

// Request is a governed tool invocation. Permission is the resolved-tool
// capability obtained from the caller's existing tool registry lookup.
type Request struct {
	Identity       entity.ExecutionIdentity
	Definition     port.ToolDefinition
	ArgumentsJSON  string
	UserID         string
	ExpectedSchema []byte
	// Ordinal is the tool invocation's index within the agent step. It is
	// persisted with the invocation so replay/audit can reproduce the sequence.
	Ordinal int

	Permission Permission
	Approval   ApprovalFunc
	Lifecycle  LifecycleFunc
}

// Permission proves that the caller resolved and permitted this exact tool.
// Its zero value is not permitted, so every execution has an explicit
// permission stage without introducing a second policy source.
type Permission struct {
	ToolName string
}

type ApprovalFunc func(context.Context) (ApprovalDecision, error)

type ApprovalDecision struct {
	Approved  bool
	DecidedBy string
	Reason    string
}

type LifecycleStage string

const (
	LifecycleValidated LifecycleStage = "validated"
	LifecycleStarted   LifecycleStage = "started"
)

type LifecycleFunc func(LifecycleStage)

// Result contains the execution result for MAGI-specific consumers such as
// evidence extraction, along with values that are safe to persist or return
// to a model.
type Result struct {
	Execution      *port.ToolExecutionResult
	Output         string
	Arguments      string
	Violations     []validation.Violation
	IdempotencyKey string
	ApprovedBy     string
	Cached         bool
}
