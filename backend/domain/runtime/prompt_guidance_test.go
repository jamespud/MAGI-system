package runtime_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

func TestBuildAgentSystemPrompt_GuidesRequiredTypesAndCustomRules(t *testing.T) {
	cfg := &entity.MagiConfig{
		Code: "melchior", Persona: "scientist",
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount: 3, MinQuantitativeCount: 1, RequiredClaimCount: 2,
			RequiredTypes: []entity.EvidenceTypeRequirement{{Type: "quantitative", MinCount: 1}},
			CustomRules: []entity.EvidenceRule{
				{Code: "primary_source_required"},
				{Code: "utility_dimension_coverage"},
			},
		},
	}
	prompt := runtime.BuildAgentSystemPrompt(cfg, nil, nil, nil, nil, true)
	if !strings.Contains(prompt, "quantitative") {
		t.Fatal("prompt should mention the required evidence type 'quantitative'")
	}
	if !strings.Contains(prompt, "primary source") {
		t.Fatal("prompt should guide the primary_source_required custom rule")
	}
	if !strings.Contains(prompt, "2 claims") || !strings.Contains(prompt, "2") {
		t.Fatal("prompt should guide utility_dimension_coverage (2 claims / 2 types)")
	}
}
