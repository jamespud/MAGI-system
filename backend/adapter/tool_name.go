package magi

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// providerToolNameMaxLen and the allowed charset follow the OpenAI-compatible
// function-name contract that model providers enforce at request time.
const providerToolNameMaxLen = 64

// pluginToolName namespaces a Coze plugin tool so it can never collide with a
// local, workflow, or MCP tool name. Local tool names are deliberately left
// untouched because tool_policy and tool_quota reference them literally.
func pluginToolName(pluginID int64, tool string) string {
	return fitToolName("plugin_" + strconv.FormatInt(pluginID, 10) + "_" + sanitizeToolSegment(tool))
}

// fitToolName keeps a name inside the provider limits. When truncation is
// needed the tail carries a digest of the full name, so two long names that
// share a prefix still map to different tool names.
func fitToolName(name string) string {
	if len(name) <= providerToolNameMaxLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:8]
	return name[:providerToolNameMaxLen-len(suffix)-1] + "_" + suffix
}

func sanitizeToolSegment(in string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(in) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
