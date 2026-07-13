package magi

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/port"
)

// WorkflowAdapter implements port.WorkflowPort via Coze crossworkflow.
// S1 skeleton; full StreamExecute wiring in S2.
type WorkflowAdapter struct{}

func NewWorkflowAdapter() *WorkflowAdapter { return &WorkflowAdapter{} }

func (a *WorkflowAdapter) Execute(ctx context.Context, workflowID string, input map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("workflow execute: not yet wired in S1 skeleton")
}

var _ port.WorkflowPort = (*WorkflowAdapter)(nil)
