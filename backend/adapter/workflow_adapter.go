package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	crossworkflow "github.com/coze-dev/coze-studio/backend/crossdomain/workflow"
	workflowmodel "github.com/coze-dev/coze-studio/backend/crossdomain/workflow/model"
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
	id, err := strconv.ParseInt(workflowID, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("workflow execute: invalid workflow ID %q", workflowID)
	}
	exec, _, err := a.svc.SyncExecuteWorkflow(ctx, workflowmodel.ExecuteConfig{
		ID:          id,
		From:        workflowmodel.FromLatestVersion,
		Mode:        workflowmodel.ExecuteModeRelease,
		TaskType:    workflowmodel.TaskTypeForeground,
		SyncPattern: workflowmodel.SyncPatternSync,
		BizType:     workflowmodel.BizTypeWorkflow,
		Cancellable: true,
	}, input)
	if err != nil {
		return nil, fmt.Errorf("workflow execute: %w", err)
	}
	if exec == nil {
		return nil, fmt.Errorf("workflow execute: empty response")
	}
	if exec.Status != crossworkflow.WorkflowSuccess {
		reason := "workflow failed"
		if exec.FailReason != nil && *exec.FailReason != "" {
			reason = *exec.FailReason
		}
		return nil, fmt.Errorf("workflow execute: %s", reason)
	}
	if exec.Output == nil || *exec.Output == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(*exec.Output), &out); err != nil {
		return map[string]any{"output": *exec.Output}, nil
	}
	return out, nil
}

var _ port.WorkflowPort = (*WorkflowAdapter)(nil)
