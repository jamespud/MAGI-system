package magi

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type recordingRuntime struct{}

func (recordingRuntime) Run(context.Context, *entity.MagiConfig, *runtime.AgentContext) (*runtime.LoopResult, error) {
	return nil, nil
}

// A sub-investigation reuses one role config, so if that config still carried
// the delegate tool a sub-run could delegate again — unbounded nesting with no
// durable identity to trace it (docs/reliability-hazard-audit.md §5.2).
func TestSubInvestigatorConfigCannotDelegate(t *testing.T) {
	cfg := &entity.MagiConfig{
		Code: "melchior",
		Tools: []entity.ToolBinding{
			{Source: entity.ToolSourceLocal, ToolName: "web_search"},
			{Source: entity.ToolSourceLocal, ToolName: DelegateToolName},
		},
	}
	if _, err := NewLoopSubInvestigator(nil, cfg); err == nil {
		t.Fatal("expected a nil-loop error")
	}

	sub, err := NewLoopSubInvestigator(recordingRuntime{}, cfg)
	if err != nil {
		t.Fatalf("build investigator: %v", err)
	}
	for _, b := range sub.cfg.Tools {
		if b.ToolName == DelegateToolName {
			t.Fatalf("sub-investigator still exposes %s: %+v", DelegateToolName, sub.cfg.Tools)
		}
	}
	if len(sub.cfg.Tools) != 1 || sub.cfg.Tools[0].ToolName != "web_search" {
		t.Fatalf("sub-investigator tools = %+v, want only web_search", sub.cfg.Tools)
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("caller config was mutated: %+v", cfg.Tools)
	}
}
