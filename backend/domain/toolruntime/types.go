package toolruntime

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

// Request is a governed tool invocation. Definition is permission-scoped by
// its caller; Permission allows an additional caller-owned admission check.
type Request struct {
	Identity       entity.ExecutionIdentity
	Definition     port.ToolDefinition
	ArgumentsJSON  string
	UserID         string
	ExpectedSchema []byte

	Permission PermissionFunc
	Approval   ApprovalFunc
	Lifecycle  LifecycleFunc
}

type PermissionFunc func(context.Context, port.ToolDefinition) error

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
