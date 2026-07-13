package magi

import (
	"context"
	"fmt"
	"sync"

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	pluginmodel "github.com/coze-dev/coze-studio/backend/crossdomain/plugin/model"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// PluginAdapter implements ToolRegistryPort + ToolExecutorPort via Coze crossplugin.
// S1 skeleton: List resolves tool names; full ArgsSchema extraction (OpenAPI3 -> JSON Schema)
// is filled in S2 when the runtime consumes it.
type PluginAdapter struct {
	svc          crossplugin.PluginService
	activated    bool
	activateOnce sync.Once
	activateErr  error
}

func NewPluginAdapter(svc crossplugin.PluginService) *PluginAdapter {
	return &PluginAdapter{svc: svc}
}

func (a *PluginAdapter) activate(ctx context.Context) error {
	a.activateOnce.Do(func() {
		_, a.activateErr = a.svc.MGetOnlineTools(ctx, []int64{})
		if a.activateErr == nil {
			a.activated = true
		}
	})
	return a.activateErr
}

func (a *PluginAdapter) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	out := make([]port.ToolDefinition, 0, len(bindings))
	for _, b := range bindings {
		if b.Source != entity.ToolSourcePlugin {
			continue
		}
		var tools []*pluginmodel.ToolInfo
		var err error
		if b.IsDraft {
			tools, err = a.svc.MGetDraftTools(ctx, []int64{b.PluginID})
		} else {
			tools, err = a.svc.MGetOnlineTools(ctx, []int64{b.PluginID})
		}
		if err != nil {
			return nil, fmt.Errorf("fetch plugin tools failed: %w", err)
		}
		for _, t := range tools {
			if t != nil && t.ID == b.ToolID {
				out = append(out, port.ToolDefinition{
					Name:    t.GetName(),
					Desc:    t.GetDesc(),
					Source:  entity.ToolSourcePlugin,
					Binding: b,
				})
			}
		}
	}
	return out, nil
}

func (a *PluginAdapter) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	if err := a.activate(ctx); err != nil {
		return nil, fmt.Errorf("plugin adapter: Coze plugin API unavailable: %w", err)
	}
	return nil, fmt.Errorf("plugin execute: binding resolution pending; Coze API confirmed available")
}
