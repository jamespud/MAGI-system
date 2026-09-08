package harnesstest

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ToolRegistry returns a fixed set of tool definitions for a set of bindings.
type ToolRegistry struct {
	Defs []port.ToolDefinition
}

func (r *ToolRegistry) List(_ context.Context, _ []entity.ToolBinding) ([]port.ToolDefinition, error) {
	return r.Defs, nil
}

var _ port.ToolRegistryPort = (*ToolRegistry)(nil)

// CountingTool counts invocations and returns a deterministic output.
type CountingTool struct {
	Calls int
	Last  port.ToolExecutionRequest
}

func (t *CountingTool) Execute(_ context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	t.Calls++
	t.Last = req
	return &port.ToolExecutionResult{Output: fmt.Sprintf("%s:ok", req.ToolName)}, nil
}

var _ port.ToolExecutorPort = (*CountingTool)(nil)

// FailOnceTool fails its first invocation and succeeds afterwards, which models
// an ambiguous then-recoverable external effect for crash recovery tests.
type FailOnceTool struct {
	Calls int
}

func (t *FailOnceTool) Execute(_ context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	t.Calls++
	if t.Calls == 1 {
		return nil, fmt.Errorf("tool failed: first attempt")
	}
	return &port.ToolExecutionResult{Output: fmt.Sprintf("%s:recovered", req.ToolName)}, nil
}

var _ port.ToolExecutorPort = (*FailOnceTool)(nil)

// SideEffectTool records external side effects and returns output. It is used
// to assert that a crash before/after a side effect never duplicates it.
type SideEffectTool struct {
	ExternalWrites int
	Calls          int
}

func (t *SideEffectTool) Execute(_ context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	t.Calls++
	t.ExternalWrites++
	return &port.ToolExecutionResult{Output: fmt.Sprintf("%s:side-effect", req.ToolName)}, nil
}

var _ port.ToolExecutorPort = (*SideEffectTool)(nil)
