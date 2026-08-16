package runtime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// staticPromptProvider returns a fixed workflow override for the tools key.
type staticPromptProvider struct{ overrides map[string]string }

func (p *staticPromptProvider) Load(_ context.Context, key string) (string, bool) {
	v, ok := p.overrides[key]
	return v, ok
}

func TestBuildAgentSystemPrompt_ProviderOverridesWorkflow(t *testing.T) {
	prov := &staticPromptProvider{overrides: map[string]string{
		"agent.workflow_tools": "CUSTOM TOOLS WORKFLOW {{EXTRA}}",
	}}
	cfg := &entity.MagiConfig{Code: "melchior", Persona: "Melchior persona", EvidenceStandard: entity.EvidenceStandard{}}
	out := runtime.BuildAgentSystemPromptCtx(context.Background(), prov, cfg, nil, nil, nil, nil, true)
	if !strings.Contains(out, "CUSTOM TOOLS WORKFLOW") {
		t.Fatalf("provider override not applied:\n%s", out)
	}
	if strings.Contains(out, "gather evidence via tool calls") {
		t.Fatal("built-in workflow text should be replaced by the override")
	}
}

func TestBuildAgentSystemPrompt_FallsBackToBuiltinWithoutProvider(t *testing.T) {
	cfg := &entity.MagiConfig{Code: "casper", Persona: "Casper persona", EvidenceStandard: entity.EvidenceStandard{}}
	out := runtime.BuildAgentSystemPromptCtx(context.Background(), nil, cfg, nil, nil, nil, nil, false)
	if !strings.Contains(out, "You have no tools available") {
		t.Fatalf("built-in no-tools workflow expected:\n%s", out)
	}
}
