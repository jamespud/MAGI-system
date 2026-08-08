package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	pluginmodel "github.com/coze-dev/coze-studio/backend/crossdomain/plugin/model"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// PluginAdapter implements ToolRegistryPort + ToolExecutorPort via Coze crossplugin.
type PluginAdapter struct {
	svc          crossplugin.PluginService
	activated    bool
	activateOnce sync.Once
	activateErr  error
	mu           sync.RWMutex
	bindings     map[string]entity.ToolBinding
}

func NewPluginAdapter(svc crossplugin.PluginService) *PluginAdapter {
	return &PluginAdapter{svc: svc, bindings: make(map[string]entity.ToolBinding)}
}

func (a *PluginAdapter) activate(ctx context.Context) error {
	a.activateOnce.Do(func() {
		if a.svc == nil {
			a.activateErr = fmt.Errorf("plugin adapter: Coze plugin service not initialized")
			return
		}
		a.activated = true
	})
	return a.activateErr
}

func (a *PluginAdapter) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	hasPlugin := false
	for _, b := range bindings {
		if b.Source == entity.ToolSourcePlugin {
			hasPlugin = true
		}
	}
	if !hasPlugin {
		return nil, nil
	}
	if err := a.activate(ctx); err != nil {
		return nil, err
	}
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
			if t == nil || t.ID != b.ToolID || t.GetName() == "" {
				continue
			}
			name := t.GetName()
			def := port.ToolDefinition{
				Name:       name,
				Desc:       t.GetDesc(),
				ArgsSchema: pluginArgsSchema(t),
				Source:     entity.ToolSourcePlugin,
				Binding:    b,
			}
			out = append(out, def)
			a.mu.Lock()
			a.bindings[name] = b
			a.mu.Unlock()
		}
	}
	return out, nil
}

func (a *PluginAdapter) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	if err := a.activate(ctx); err != nil {
		return nil, err
	}
	if req.UserID == "" {
		return nil, fmt.Errorf("plugin execute: user ID is required")
	}
	binding := req.Binding
	if binding.Source != entity.ToolSourcePlugin || binding.PluginID == 0 || binding.ToolID == 0 {
		a.mu.RLock()
		binding, _ = a.bindings[req.ToolName]
		a.mu.RUnlock()
	}
	if binding.Source != entity.ToolSourcePlugin || binding.PluginID == 0 || binding.ToolID == 0 {
		return nil, fmt.Errorf("plugin execute: no binding for tool %q", req.ToolName)
	}
	resp, err := a.svc.ExecuteTool(ctx, &pluginmodel.ExecuteToolRequest{
		UserID:          req.UserID,
		PluginID:        binding.PluginID,
		ToolID:          binding.ToolID,
		ExecDraftTool:   binding.IsDraft,
		ArgumentsInJson: req.ArgumentsJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("plugin execute %q: %w", req.ToolName, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("plugin execute %q: empty response", req.ToolName)
	}
	return &port.ToolExecutionResult{
		Output:     resp.TrimmedResp,
		Structured: resp.TrimmedResp,
		Raw:        resp.RawResp,
	}, nil
}

func pluginArgsSchema(t *pluginmodel.ToolInfo) []byte {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	if t != nil && t.Operation != nil {
		_, ref := t.Operation.GetReqBodySchema()
		if ref != nil && ref.Value != nil {
			schema = openAPISchema(ref.Value)
		}
	}
	data, _ := json.Marshal(schema)
	return data
}

func openAPISchema(s *openapi3.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object"}
	}
	out := make(map[string]any)
	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, ref := range s.Properties {
			if ref != nil && ref.Value != nil {
				props[name] = openAPISchema(ref.Value)
			}
		}
		out["properties"] = props
	}
	if s.Items != nil && s.Items.Value != nil {
		out["items"] = openAPISchema(s.Items.Value)
	}
	return out
}

var _ port.ToolRegistryPort = (*PluginAdapter)(nil)
var _ port.ToolExecutorPort = (*PluginAdapter)(nil)
