package magi

import (
	"context"
	"fmt"

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	pluginmodel "github.com/coze-dev/coze-studio/backend/crossdomain/plugin/model"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// PluginAdapter implements ToolRegistryPort + ToolExecutorPort via Coze crossplugin.
// S1 skeleton: List resolves tool names; full ArgsSchema extraction (OpenAPI3 -> JSON Schema)
// is filled in S2 when the runtime consumes it.
type PluginAdapter struct {
	svc crossplugin.PluginService
}

func NewPluginAdapter(svc crossplugin.PluginService) *PluginAdapter {
	return &PluginAdapter{svc: svc}
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
	// Binding lookup is the runtime's responsibility; here Execute requires the binding
	// carried via ToolDefinition. S2 will thread the binding through.
	return nil, fmt.Errorf("plugin execute: binding resolution not yet wired in S1 skeleton")
}
