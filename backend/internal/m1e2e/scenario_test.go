package m1e2e

import (
	"reflect"
	"testing"
)

func TestLoadScenarioMatchesFrozenM1Inputs(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatalf("LoadScenario() error = %v", err)
	}
	if scenario.ScenarioID != "ROLLOUT-LAB-E2E-01" || scenario.Version != "1.0" {
		t.Fatalf("scenario identity = %s/%s", scenario.ScenarioID, scenario.Version)
	}
	if scenario.CaseID != "case-rollout-lab-e2e-01" || scenario.Round != 1 || scenario.ExecutionGeneration != 1 {
		t.Fatalf("case binding = %s round=%d generation=%d", scenario.CaseID, scenario.Round, scenario.ExecutionGeneration)
	}
	if scenario.RiskLevel != "R0" || scenario.ProtectedWritesPermitted {
		t.Fatalf("risk boundary = %q protected=%v, want R0 and false", scenario.RiskLevel, scenario.ProtectedWritesPermitted)
	}

	wantPersonas := []string{"melchior", "balthasar", "casper"}
	gotPersonas := make([]string, 0, len(scenario.Personas))
	for _, persona := range scenario.Personas {
		gotPersonas = append(gotPersonas, persona.PersonaID)
		if persona.SpecVersion != "1.0" || persona.MaxToolCalls != 6 || persona.MaxOutputTokens != 6000 {
			t.Fatalf("persona budget = %+v", persona)
		}
		if len(persona.MinimumInvestigations) < 2 {
			t.Fatalf("persona %s has no minimum investigations", persona.PersonaID)
		}
	}
	if !reflect.DeepEqual(gotPersonas, wantPersonas) {
		t.Fatalf("personas = %v, want %v", gotPersonas, wantPersonas)
	}

	wantCandidates := []string{"C0", "C1", "C2", "C3"}
	gotCandidates := make([]string, 0, len(scenario.Candidates))
	noAction := 0
	for _, candidate := range scenario.Candidates {
		gotCandidates = append(gotCandidates, candidate.CandidateID)
		if candidate.IsNoAction {
			noAction++
		}
	}
	if !reflect.DeepEqual(gotCandidates, wantCandidates) || noAction != 1 {
		t.Fatalf("candidates = %v no_action=%d", gotCandidates, noAction)
	}

	wantNegative := []string{
		"missing_third_receipt",
		"stale_material",
		"revoked_context",
		"duplicate_ballot",
		"witness_unavailable",
		"provider_outcome_unknown",
		"old_generation_replay",
		"protected_send_attempt_r0",
	}
	gotNegative := make([]string, 0, len(scenario.NegativeCases))
	for _, negative := range scenario.NegativeCases {
		gotNegative = append(gotNegative, negative.ID)
		if negative.ExpectedStatus != StatusBlocked && negative.ExpectedStatus != StatusFail {
			t.Fatalf("negative case %s has non-failing expectation %q", negative.ID, negative.ExpectedStatus)
		}
	}
	if !reflect.DeepEqual(gotNegative, wantNegative) {
		t.Fatalf("negative cases = %v, want %v", gotNegative, wantNegative)
	}

	if len(scenario.ArtifactSlots) < 10 {
		t.Fatalf("artifact slots = %d, want at least 10", len(scenario.ArtifactSlots))
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("fixture validation error = %v", err)
	}
}

func TestScenarioValidateRejectsUnsafeOrIncompleteInputs(t *testing.T) {
	base, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Scenario)
	}{
		{
			name: "protected write in R0",
			mutate: func(s *Scenario) {
				s.ProtectedWritesPermitted = true
			},
		},
		{
			name: "missing persona",
			mutate: func(s *Scenario) {
				s.Personas = s.Personas[:2]
			},
		},
		{
			name: "duplicate candidate",
			mutate: func(s *Scenario) {
				s.Candidates[1].CandidateID = s.Candidates[0].CandidateID
			},
		},
		{
			name: "unknown stage",
			mutate: func(s *Scenario) {
				s.Stages = append(s.Stages, StageSpec{ID: "UNKNOWN"})
			},
		},
		{
			name: "missing required negative case",
			mutate: func(s *Scenario) {
				s.NegativeCases = s.NegativeCases[:len(s.NegativeCases)-1]
			},
		},
		{
			name: "duplicate artifact slot",
			mutate: func(s *Scenario) {
				s.ArtifactSlots = append(s.ArtifactSlots, s.ArtifactSlots[0])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario := base
			scenario.Personas = append([]PersonaSpec(nil), base.Personas...)
			scenario.Candidates = append([]Candidate(nil), base.Candidates...)
			scenario.Stages = append([]StageSpec(nil), base.Stages...)
			scenario.NegativeCases = append([]NegativeCase(nil), base.NegativeCases...)
			scenario.ArtifactSlots = append([]ArtifactSlot(nil), base.ArtifactSlots...)
			tt.mutate(&scenario)
			if err := scenario.Validate(); err == nil {
				t.Fatal("invalid scenario accepted")
			}
		})
	}
}
