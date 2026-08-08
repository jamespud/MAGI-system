package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ToolRegistryMux routes binding resolution across local, plugin, workflow,
// and code-runner tool sources.
type ToolRegistryMux struct {
	local      port.ToolRegistryPort
	plugin     port.ToolRegistryPort
	workflow   port.WorkflowPort
	coderunner port.CodeRunnerPort
}

func NewToolRegistryMux(local, plugin port.ToolRegistryPort) *ToolRegistryMux {
	return &ToolRegistryMux{local: local, plugin: plugin}
}

func NewToolRegistryMuxWithExt(local, plugin port.ToolRegistryPort, workflow port.WorkflowPort, coderunner port.CodeRunnerPort) *ToolRegistryMux {
	return &ToolRegistryMux{local: local, plugin: plugin, workflow: workflow, coderunner: coderunner}
}

func (m *ToolRegistryMux) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	var out []port.ToolDefinition
	if m.local != nil {
		defs, err := m.local.List(ctx, bindings)
		if err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	if m.plugin != nil {
		defs, err := m.plugin.List(ctx, bindings)
		if err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	for _, b := range bindings {
		switch b.Source {
		case entity.ToolSourceWorkflow:
			if m.workflow == nil || b.WorkflowID <= 0 {
				continue
			}
			out = append(out, port.ToolDefinition{
				Name:       fmt.Sprintf("workflow_%d", b.WorkflowID),
				Desc:       "Execute a saved workflow with a JSON object input.",
				ArgsSchema: []byte(`{"type":"object","additionalProperties":true}`),
				Source:     entity.ToolSourceWorkflow,
				Binding:    b,
			})
		case entity.ToolSourceCodeRunner:
			if m.coderunner == nil {
				continue
			}
			out = append(out, port.ToolDefinition{
				Name:       "code_runner",
				Desc:       "Run Python or JavaScript code in the sandbox and return the JSON result.",
				ArgsSchema: []byte(`{"type":"object","properties":{"lang":{"type":"string","enum":["Python","JavaScript"]},"code":{"type":"string"}},"required":["lang","code"],"additionalProperties":false}`),
				Source:     entity.ToolSourceCodeRunner,
				Binding:    b,
			})
		}
	}
	return out, nil
}

// ToolExecutorMux routes execution using the binding attached by the runtime.
type ToolExecutorMux struct {
	local      port.ToolExecutorPort
	plugin     port.ToolExecutorPort
	workflow   port.WorkflowPort
	coderunner port.CodeRunnerPort
}

func NewToolExecutorMux(local, plugin port.ToolExecutorPort) *ToolExecutorMux {
	return &ToolExecutorMux{local: local, plugin: plugin}
}

func NewToolExecutorMuxWithExt(local, plugin port.ToolExecutorPort, workflow port.WorkflowPort, coderunner port.CodeRunnerPort) *ToolExecutorMux {
	return &ToolExecutorMux{local: local, plugin: plugin, workflow: workflow, coderunner: coderunner}
}

func (m *ToolExecutorMux) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	switch req.Binding.Source {
	case entity.ToolSourcePlugin:
		if m.plugin == nil {
			return nil, fmt.Errorf("tool executor: plugin executor is not configured")
		}
		return m.plugin.Execute(ctx, req)
	case entity.ToolSourceWorkflow:
		if m.workflow == nil {
			return nil, fmt.Errorf("tool executor: workflow executor is not configured")
		}
		var input map[string]any
		if req.ArgumentsJSON != "" {
			if err := json.Unmarshal([]byte(req.ArgumentsJSON), &input); err != nil {
				return nil, fmt.Errorf("workflow tool: parse args: %w", err)
			}
		}
		out, err := m.workflow.Execute(ctx, strconv.FormatInt(req.Binding.WorkflowID, 10), input)
		if err != nil {
			return nil, fmt.Errorf("workflow tool %d: %w", req.Binding.WorkflowID, err)
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("workflow tool: encode result: %w", err)
		}
		return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
	case entity.ToolSourceCodeRunner:
		if m.coderunner == nil {
			return nil, fmt.Errorf("tool executor: code runner is not configured")
		}
		var args struct {
			Lang string `json:"lang"`
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
			return nil, fmt.Errorf("code runner tool: parse args: %w", err)
		}
		out, err := m.coderunner.Run(ctx, args.Lang, args.Code)
		if err != nil {
			return nil, fmt.Errorf("code runner tool: %w", err)
		}
		return &port.ToolExecutionResult{Output: out}, nil
	default:
		if m.local == nil {
			return nil, fmt.Errorf("tool executor: local executor is not configured")
		}
		return m.local.Execute(ctx, req)
	}
}

var _ port.ToolRegistryPort = (*ToolRegistryMux)(nil)
var _ port.ToolExecutorPort = (*ToolExecutorMux)(nil)
