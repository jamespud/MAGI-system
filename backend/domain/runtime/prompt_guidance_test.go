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

func TestBuildAgentSystemPrompt_UsesStructuredPersonaAndRoleContract(t *testing.T) {
	policy := entity.DefaultRolePolicy("balthasar")
	cfg := &entity.MagiConfig{
		Code:       "balthasar",
		Persona:    "legacy persona should not win",
		PersonaDef: &entity.PersonaDefinition{SystemPrompt: "structured protector", Voice: "terse and operational"},
		RolePolicy: policy,
	}
	prompt := runtime.BuildAgentSystemPrompt(cfg, nil, nil, nil, nil, false)
	if !strings.Contains(prompt, "structured protector") || strings.Contains(prompt, "legacy persona should not win") {
		t.Fatalf("prompt should use structured persona definition, got %s", prompt)
	}
	for _, want := range []string{"role_assessment.risk", "worst_case", "residual_risk", "rollback_plan", "reversibility_score"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("Balthasar prompt should contain %q, got %s", want, prompt)
		}
	}
}

func TestBuildAgentSystemPrompt_RoleContractsDiffer(t *testing.T) {
	m := runtime.BuildAgentSystemPrompt(&entity.MagiConfig{Code: "melchior", RolePolicy: entity.DefaultRolePolicy("melchior")}, nil, nil, nil, nil, false)
	b := runtime.BuildAgentSystemPrompt(&entity.MagiConfig{Code: "balthasar", RolePolicy: entity.DefaultRolePolicy("balthasar")}, nil, nil, nil, nil, false)
	c := runtime.BuildAgentSystemPrompt(&entity.MagiConfig{Code: "casper", RolePolicy: entity.DefaultRolePolicy("casper")}, nil, nil, nil, nil, false)
	if strings.Contains(m, "rollback_plan") || strings.Contains(m, "opportunity_cost") || !strings.Contains(m, "quantitative_evidence_ids") {
		t.Fatalf("Melchior prompt has the wrong role contract: %s", m)
	}
	if strings.Contains(b, "quantitative_evidence_ids") || strings.Contains(b, "opportunity_cost") || !strings.Contains(b, "rollback_plan") {
		t.Fatalf("Balthasar prompt has the wrong role contract: %s", b)
	}
	if strings.Contains(c, "quantitative_evidence_ids") || strings.Contains(c, "rollback_plan") || !strings.Contains(c, "opportunity_cost") {
		t.Fatalf("Casper prompt has the wrong role contract: %s", c)
	}
}

func TestBuildAgentSystemPrompt_TargetsRoleSpecificDebateQuestion(t *testing.T) {
	prompt := runtime.BuildAgentSystemPrompt(
		&entity.MagiConfig{Code: "casper", RolePolicy: entity.DefaultRolePolicy("casper")},
		nil, nil, nil,
		&runtime.DebateContext{Packet: entity.DebatePacket{Questions: []entity.DebateQuestion{
			{To: entity.MagiCodeMelchior, Text: "technical question"},
			{To: entity.MagiCodeCasper, Text: "opportunity question"},
		}}},
		false,
	)
	if !strings.Contains(prompt, "opportunity question") || strings.Contains(prompt, "technical question") {
		t.Fatalf("Casper prompt should receive only its targeted debate question: %s", prompt)
	}
}
