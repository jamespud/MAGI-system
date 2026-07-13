package magi

import "github.com/jamespud/magi/backend/domain/port"

// Service wires MAGI ports to Coze-backed adapters (ADR-007). Constructed once
// at application bootstrap; domain/magi consumes only the port interfaces.
type Service struct {
	Model      port.ModelPort
	ToolReg    port.ToolRegistryPort
	ToolExec   port.ToolExecutorPort
	Knowledge  port.KnowledgePort
	Workflow   port.WorkflowPort
	CodeRunner port.CodeRunnerPort
	Events     port.EventPublisher
}
