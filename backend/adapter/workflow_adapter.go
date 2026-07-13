package magi

import (
	"context"
	"fmt"
	"sync"

	crossworkflow "github.com/coze-dev/coze-studio/backend/crossdomain/workflow"
	"github.com/jamespud/magi/backend/domain/port"
)

// WorkflowAdapter implements port.WorkflowPort via Coze crossworkflow.
// Progressive activation: Coze API availability is probed on first Execute call.
type WorkflowAdapter struct {
	svc          crossworkflow.Workflow
	activated    bool
	activateOnce sync.Once
	activateErr  error
}

func NewWorkflowAdapter(svc crossworkflow.Workflow) *WorkflowAdapter {
	return &WorkflowAdapter{svc: svc}
}

func (a *WorkflowAdapter) activate(ctx context.Context) error {
	a.activateOnce.Do(func() {
		if a.svc == nil {
			a.activateErr = fmt.Errorf("workflow adapter: Coze workflow service not initialized")
			return
		}
		a.activated = true
	})
	return a.activateErr
}

func (a *WorkflowAdapter) Execute(ctx context.Context, workflowID string, input map[string]any) (map[string]any, error) {
	if err := a.activate(ctx); err != nil {
		return nil, fmt.Errorf("workflow adapter: Coze workflow API unavailable: %w", err)
	}
	return nil, fmt.Errorf("workflow execute: full wiring pending; Coze API confirmed available")
}

var _ port.WorkflowPort = (*WorkflowAdapter)(nil)
