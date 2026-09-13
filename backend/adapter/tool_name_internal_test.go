package magi

import (
	"regexp"
	"strings"
	"testing"
)

func TestPluginToolName_IsNamespacedAndProviderSafe(t *testing.T) {
	got := pluginToolName(1234, "Get Weather!")
	if got != "plugin_1234_get_weather" {
		t.Fatalf("name = %q, want plugin_1234_get_weather", got)
	}

	// Provider function names must match ^[a-zA-Z0-9_-]{1,64}$.
	allowed := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	long := pluginToolName(987654321, strings.Repeat("very_long_tool_name_", 6))
	if len(long) > providerToolNameMaxLen {
		t.Fatalf("name length = %d, want <= %d (%q)", len(long), providerToolNameMaxLen, long)
	}
	if !allowed.MatchString(long) {
		t.Fatalf("name %q violates the provider pattern", long)
	}
	// Distinct inputs must not collapse onto the same truncated name.
	other := fitToolName(long + "_other")
	if other == long {
		t.Fatalf("truncation collided: %q", other)
	}
}

func TestPluginToolName_SeparatesPlugins(t *testing.T) {
	a := pluginToolName(1, "search")
	b := pluginToolName(2, "search")
	if a == b {
		t.Fatalf("two plugins produced the same tool name %q", a)
	}
}
