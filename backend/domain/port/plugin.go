package port

import (
	"context"
	"errors"
)

// ErrPluginToolNotFound means the referenced plugin tool does not exist (or is
// not visible to this deployment).
var ErrPluginToolNotFound = errors.New("plugin tool not found")

// PluginToolResolver confirms that a (plugin, tool) pair resolves to a real tool
// before a binding is persisted, so a typo or a revoked plugin surfaces as an
// error instead of a binding that silently exposes nothing.
type PluginToolResolver interface {
	ResolveToolName(ctx context.Context, pluginID, toolID int64, isDraft bool) (string, error)
}
