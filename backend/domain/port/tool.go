package port

import (
	"context"
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ToolDefinition is a resolved tool's schema + source metadata.
type ToolDefinition struct {
	Name        string
	Desc        string
	ArgsSchema  []byte // JSON Schema (unified IR, ADR-003)
	Source      entity.ToolSource
	Binding     entity.ToolBinding
	EffectClass ToolEffectClass
}

// ToolEffectClass describes the external side-effect semantics of a tool.
// The zero value is deliberately treated as ToolEffectUnknown by runtimes.
type ToolEffectClass string

const (
	ToolEffectReadOnly      ToolEffectClass = "read_only"
	ToolEffectIdempotent    ToolEffectClass = "idempotent"
	ToolEffectNonIdempotent ToolEffectClass = "non_idempotent"
	ToolEffectUnknown       ToolEffectClass = "unknown"
)

// ParseToolEffectClass converts a configured effect-class name. It is the only
// parser: config validation and adapter wiring share it, so a value that passes
// validation can never be rejected later.
func ParseToolEffectClass(value string) (ToolEffectClass, error) {
	switch effect := ToolEffectClass(strings.TrimSpace(value)); effect {
	case ToolEffectReadOnly, ToolEffectIdempotent, ToolEffectNonIdempotent, ToolEffectUnknown:
		return effect, nil
	default:
		return "", fmt.Errorf("unknown tool effect class %q", value)
	}
}

// ToolExecutionRequest is a request to execute a bound tool.
type ToolExecutionRequest struct {
	ToolName      string
	ArgumentsJSON string
	UserID        string
	Binding       entity.ToolBinding
	// EffectClass is the resolved side-effect semantics of the tool. Executors
	// that may re-invoke an external system must consult it: only read_only and
	// idempotent tools may be re-invoked after an ambiguous failure, and an
	// executor that cannot tell whether its call landed must report
	// execution.ErrExternalOutcomeUnknown rather than a plain failure.
	EffectClass ToolEffectClass
	// ExpectedSchema is the authoritative output schema for the current phase.
	// Optional; only set by the agent loop for check_output so models need not
	// reproduce it. Other tools ignore it.
	ExpectedSchema []byte

	RunID          string
	StepID         string
	InvocationID   string
	AttemptID      string
	IdempotencyKey string
}

// ToolExecutionResult is the raw tool output.
type ToolExecutionResult struct {
	Output     string
	Structured any
	Raw        any
	SourceURI  string
}

// ToolRegistryPort resolves bindings to tool definitions (schema for model binding).
type ToolRegistryPort interface {
	List(ctx context.Context, bindings []entity.ToolBinding) ([]ToolDefinition, error)
}

// ToolExecutorPort executes a tool by name with validated args.
type ToolExecutorPort interface {
	Execute(ctx context.Context, req ToolExecutionRequest) (*ToolExecutionResult, error)
}
