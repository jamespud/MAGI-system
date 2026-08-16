package prompt_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/prompt"
)

func TestRender_SubstitutesKnownPlaceholders(t *testing.T) {
	out := prompt.Render("Hello {{NAME}}, you are {{ROLE}}.", map[string]string{
		"NAME": "Magi", "ROLE": "Melchior",
	})
	if out != "Hello Magi, you are Melchior." {
		t.Fatalf("render mismatch: %q", out)
	}
}

func TestRender_LeavesUnknownPlaceholdersIntact(t *testing.T) {
	out := prompt.Render("{{KNOWN}} {{UNKNOWN}}", map[string]string{"KNOWN": "x"})
	if out != "x {{UNKNOWN}}" {
		t.Fatalf("unknown placeholder must survive, got %q", out)
	}
}

func TestDefaults_CoversAllKeys(t *testing.T) {
	def := prompt.Default()
	for _, k := range []string{
		prompt.KeyCommanderNormalize, prompt.KeyCommanderReport,
		prompt.KeyAgentWorkflowTools, prompt.KeyAgentWorkflowNoTools,
	} {
		if _, ok := def[k]; !ok || def[k] == "" {
			t.Fatalf("missing built-in template for %s", k)
		}
	}
}
