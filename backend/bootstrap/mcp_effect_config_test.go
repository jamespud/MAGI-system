package bootstrap

import (
	"strings"
	"testing"
)

func mcpConfigWithEffect(effect string) *Config {
	cfg := validMCPBaseConfig()
	cfg.MCP.Servers = []MCPServerConfig{{
		Name:            "srv",
		Transport:       "stdio",
		Command:         "tool",
		EffectOverrides: map[string]string{"search": effect},
	}}
	return cfg
}

// validMCPBaseConfig is the smallest config that passes the unrelated model
// checks, so these tests can assert on the MCP section alone.
func validMCPBaseConfig() *Config {
	cfg := &Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1
	return cfg
}

// A typo in effect_overrides would otherwise silently leave the tool
// unclassified, which the retry policy treats as unsafe — the config would look
// like it did something while the tool stayed fail-closed.
func TestValidate_RejectsUnknownEffectOverride(t *testing.T) {
	err := mcpConfigWithEffect("readonly").Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown tool effect class") {
		t.Fatalf("validate error = %v, want an unknown effect class error", err)
	}
}

func TestValidate_AcceptsKnownEffectOverrides(t *testing.T) {
	for _, effect := range []string{"read_only", "idempotent", "non_idempotent", "unknown"} {
		if err := mcpConfigWithEffect(effect).Validate(); err != nil {
			t.Fatalf("effect %q rejected: %v", effect, err)
		}
	}
}

// retry_attempts was only validated on the http branch, so a stdio server could
// carry a negative value that silently meant "attempt once".
func TestValidate_RejectsNegativeRetryAttemptsForStdio(t *testing.T) {
	cfg := validMCPBaseConfig()
	cfg.MCP.Servers = []MCPServerConfig{{
		Name: "srv", Transport: "stdio", Command: "tool", RetryAttempts: -1,
	}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "retry_attempts") {
		t.Fatalf("validate error = %v, want a retry_attempts error", err)
	}
}
