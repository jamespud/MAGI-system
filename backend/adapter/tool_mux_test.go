package magi

import (
	"context"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubWorkflowPort struct {
	id    string
	input map[string]any
}

func (s *stubWorkflowPort) Execute(ctx context.Context, workflowID string, input map[string]any) (map[string]any, error) {
	s.id = workflowID
	s.input = input
	return map[string]any{"result": "ok"}, nil
}

type stubCodeRunnerPort struct {
	lang string
	code string
}

func (s *stubCodeRunnerPort) Run(ctx context.Context, lang, code string) (string, error) {
	s.lang = lang
	s.code = code
	return `{"sum":3}`, nil
}

type stubLocalExec struct{}

func (s *stubLocalExec) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	return &port.ToolExecutionResult{Output: "local:" + req.ToolName}, nil
}

func TestToolRegistryMux_ListsWorkflowAndCodeRunner(t *testing.T) {
	m := NewToolRegistryMuxWithExt(nil, nil, &stubWorkflowPort{}, &stubCodeRunnerPort{})
	defs, err := m.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceWorkflow, WorkflowID: 42},
		{Source: entity.ToolSourceCodeRunner, ToolName: "code_runner"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
	if defs[0].Name != "workflow_42" || defs[0].Source != entity.ToolSourceWorkflow {
		t.Fatalf("unexpected workflow def: %+v", defs[0])
	}
	if defs[1].Name != "code_runner" || defs[1].Source != entity.ToolSourceCodeRunner {
		t.Fatalf("unexpected coderunner def: %+v", defs[1])
	}
}

func TestToolRegistryMux_WithoutPortsSkipsExtSources(t *testing.T) {
	m := NewToolRegistryMux(nil, nil)
	defs, err := m.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceWorkflow, WorkflowID: 42},
		{Source: entity.ToolSourceCodeRunner},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected no ext defs without ports, got %+v", defs)
	}
}

func TestToolExecutorMux_RoutesWorkflow(t *testing.T) {
	wf := &stubWorkflowPort{}
	m := NewToolExecutorMuxWithExt(nil, nil, wf, nil)
	res, err := m.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "workflow_42", ArgumentsJSON: `{"a":1}`,
		Binding: entity.ToolBinding{Source: entity.ToolSourceWorkflow, WorkflowID: 42},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if wf.id != "42" {
		t.Fatalf("workflow id: %s", wf.id)
	}
	if wf.input["a"] != float64(1) {
		t.Fatalf("input: %+v", wf.input)
	}
	if !strings.Contains(res.Output, `"result":"ok"`) {
		t.Fatalf("output: %s", res.Output)
	}
}

func TestToolExecutorMux_RoutesCodeRunner(t *testing.T) {
	cr := &stubCodeRunnerPort{}
	m := NewToolExecutorMuxWithExt(nil, nil, nil, cr)
	res, err := m.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "code_runner", ArgumentsJSON: `{"lang":"Python","code":"print(1)"}`,
		Binding: entity.ToolBinding{Source: entity.ToolSourceCodeRunner},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if cr.lang != "Python" || cr.code != "print(1)" {
		t.Fatalf("args: lang=%q code=%q", cr.lang, cr.code)
	}
	if res.Output != `{"sum":3}` {
		t.Fatalf("output: %s", res.Output)
	}
}

func TestToolExecutorMux_FallsBackToLocal(t *testing.T) {
	m := NewToolExecutorMuxWithExt(&stubLocalExec{}, nil, nil, nil)
	res, err := m.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search",
		Binding:  entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output != "local:web_search" {
		t.Fatalf("output: %s", res.Output)
	}
}
