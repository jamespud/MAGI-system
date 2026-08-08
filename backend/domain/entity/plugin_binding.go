package entity

import "time"

// PluginBinding is a user-scoped declaration of an enabled Coze plugin tool.
// Agent runs for that user resolve their tool set from these bindings plus
// the built-in local tools (web_search).
type PluginBinding struct {
	ID       string
	UserID   int64
	PluginID int64
	ToolID   int64
	IsDraft  bool
	Enabled  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ToolBinding converts the persisted binding into the runtime tool-binding
// shape consumed by the tool registry/executor mux.
func (b *PluginBinding) ToolBinding() ToolBinding {
	return ToolBinding{
		Source:   ToolSourcePlugin,
		PluginID: b.PluginID,
		ToolID:   b.ToolID,
		IsDraft:  b.IsDraft,
	}
}
