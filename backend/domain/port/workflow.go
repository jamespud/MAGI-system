package port

import "context"

// WorkflowPort wraps Coze crossworkflow to invoke a saved workflow as a tool.
type WorkflowPort interface {
	Execute(ctx context.Context, workflowID string, input map[string]any) (map[string]any, error)
}
