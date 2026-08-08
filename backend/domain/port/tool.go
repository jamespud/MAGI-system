package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ToolDefinition is a resolved tool's schema + source metadata.
type ToolDefinition struct {
	Name       string
	Desc       string
	ArgsSchema []byte // JSON Schema (unified IR, ADR-003)
	Source     entity.ToolSource
	Binding    entity.ToolBinding
}

// ToolExecutionRequest is a request to execute a bound tool.
type ToolExecutionRequest struct {
	ToolName      string
	ArgumentsJSON string
	UserID        string
	Binding       entity.ToolBinding
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
