package m1e2e

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type stageExecutorFunc func(context.Context, Scenario, StageSpec) (StageExecution, error)

func (f stageExecutorFunc) Execute(ctx context.Context, scenario Scenario, stage StageSpec) (StageExecution, error) {
	return f(ctx, scenario, stage)
}

func readyExecutor(evidence EvidenceClass, artifacts ...ArtifactRef) StageExecutor {
	return stageExecutorFunc(func(_ context.Context, _ Scenario, _ StageSpec) (StageExecution, error) {
		return StageExecution{
			Status:        StatusEvidenceReady,
			EvidenceClass: evidence,
			Artifacts:     artifacts,
			Assertions: []Assertion{{
				ID: "stage-contract", Passed: true, Detail: "focused assertion passed",
			}},
		}, nil
	})
}

func allCapabilities() Capabilities {
	return Capabilities{
		CapabilityScenarioFixture:  true,
		CapabilityCharterContext:   true,
		CapabilityJournalAdmission: true,
		CapabilityProviderDelivery: true,
		CapabilityPersonaRuntime:   true,
		CapabilityEvidenceClosure:  true,
		CapabilitySealedBallots:    true,
		CapabilityDecisionRecord:   true,
		CapabilityTrustedReport:    true,
		CapabilityR0EgressProbe:    true,
	}
}

func allExecutors(scenario Scenario, evidence EvidenceClass) map[StageID]StageExecutor {
	executors := make(map[StageID]StageExecutor)
	slots := make(map[string]ArtifactSlot, len(scenario.ArtifactSlots))
	for _, slot := range scenario.ArtifactSlots {
		slots[slot.Name] = slot
	}
	for _, stage := range scenario.Stages {
		if stage.ID == StageScenarioContract || stage.ID == StageM1Gate {
			continue
		}
		artifacts := make([]ArtifactRef, 0, len(stage.RequiredArtifacts))
		for i, name := range stage.RequiredArtifacts {
			slot := slots[name]
			hexChar := byte('a' + i%6)
			artifacts = append(artifacts, ArtifactRef{
				Slot:   name,
				ID:     slot.IDPrefix + "_" + strings.Repeat(string(hexChar), 64),
				Digest: repeatHex(hexChar),
				Source: "focused-test-executor",
			})
		}
		executors[stage.ID] = readyExecutor(evidence, artifacts...)
	}
	return executors
}

func repeatHex(ch byte) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = ch
	}
	return string(out)
}

func TestRunnerRealModeBlocksMissingCapabilities(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Runner{
		Scenario:     scenario,
		Mode:         ModeReal,
		Capabilities: Capabilities{},
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusBlocked {
		t.Fatalf("overall status = %q, want BLOCKED", result.Status)
	}
	if result.M1Verdict != VerdictNotEvaluated {
		t.Fatalf("M1 verdict = %q, want NOT_EVALUATED", result.M1Verdict)
	}
	if len(result.Stages) != len(scenario.Stages) {
		t.Fatalf("stages = %d, want %d", len(result.Stages), len(scenario.Stages))
	}
	for _, stage := range result.Stages {
		if stage.ID == StageScenarioContract {
			if stage.Status != StatusEvidenceReady {
				t.Fatalf("scenario contract status = %q, want EVIDENCE_READY", stage.Status)
			}
			continue
		}
		if stage.ID == StageM1Gate {
			if stage.Status != StatusNotRun {
				t.Fatalf("M1 gate status = %q, want NOT_RUN", stage.Status)
			}
			continue
		}
		if stage.Status != StatusBlocked || stage.Blocker == "" {
			t.Fatalf("stage %s = %+v, want explicit BLOCKED blocker", stage.ID, stage)
		}
	}
	if result.Stages[len(result.Stages)-1].ID != StageM1Gate {
		t.Fatal("M1 gate must remain the final, non-executed stage")
	}
}

func TestRunnerSyntheticModeDowngradesRealEvidenceAndNeverPasses(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Runner{
		Scenario:     scenario,
		Mode:         ModeSynthetic,
		Capabilities: allCapabilities(),
		Executors:    allExecutors(scenario, EvidenceReal),
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusDiagnosticOnly {
		t.Fatalf("overall status = %q, want DIAGNOSTIC_ONLY", result.Status)
	}
	if result.M1Verdict != VerdictNotEvaluated {
		t.Fatalf("M1 verdict = %q, want NOT_EVALUATED", result.M1Verdict)
	}
	for _, stage := range result.Stages {
		if stage.ID == StageM1Gate {
			if stage.Status != StatusNotRun {
				t.Fatalf("M1 gate status = %q", stage.Status)
			}
			continue
		}
		if stage.Status != StatusDiagnosticOnly {
			t.Fatalf("stage %s status = %q, want DIAGNOSTIC_ONLY", stage.ID, stage.Status)
		}
	}
}

func TestRunnerRealModeRejectsSyntheticEvidence(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Runner{
		Scenario:     scenario,
		Mode:         ModeReal,
		Capabilities: allCapabilities(),
		Executors:    allExecutors(scenario, EvidenceSynthetic),
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusFail {
		t.Fatalf("overall status = %q, want FAIL", result.Status)
	}
	for _, stage := range result.Stages {
		if stage.ID == StageScenarioContract || stage.ID == StageM1Gate {
			continue
		}
		if stage.Status != StatusFail || stage.Blocker != "SYNTHETIC_EVIDENCE_IN_REAL_MODE" {
			t.Fatalf("stage %s = %+v, want synthetic-evidence failure", stage.ID, stage)
		}
	}
}

func TestRunnerAcceptsRealEvidenceOnlyAsEvidenceReady(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Runner{
		Scenario:     scenario,
		Mode:         ModeReal,
		Capabilities: allCapabilities(),
		Executors:    allExecutors(scenario, EvidenceReal),
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusEvidenceReady {
		t.Fatalf("overall status = %q, want EVIDENCE_READY", result.Status)
	}
	if result.M1Verdict != VerdictNotEvaluated {
		t.Fatalf("M1 verdict = %q, want NOT_EVALUATED", result.M1Verdict)
	}
	if result.Stages[len(result.Stages)-1].Status != StatusNotRun {
		t.Fatal("runner executed the independent M1 gate")
	}
}

func TestRunnerRejectsPassM1FromStageExecutor(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	executors := allExecutors(scenario, EvidenceReal)
	executors[StagePersonaInvestigation] = stageExecutorFunc(func(context.Context, Scenario, StageSpec) (StageExecution, error) {
		return StageExecution{Status: StatusPassM1, EvidenceClass: EvidenceReal}, nil
	})
	_, err = (Runner{
		Scenario:     scenario,
		Mode:         ModeReal,
		Capabilities: allCapabilities(),
		Executors:    executors,
	}).Run(context.Background())
	if !errors.Is(err, ErrPassM1Forbidden) {
		t.Fatalf("Run() error = %v, want ErrPassM1Forbidden", err)
	}
}

func TestRunnerRejectsMalformedArtifactIdentity(t *testing.T) {
	for _, artifactID := range []string{"x", "ctxm_" + strings.Repeat("a", 64)} {
		t.Run(artifactID, func(t *testing.T) {
			scenario, err := LoadScenario()
			if err != nil {
				t.Fatal(err)
			}
			executors := allExecutors(scenario, EvidenceReal)
			executors[StageCharterContext] = stageExecutorFunc(func(_ context.Context, _ Scenario, stage StageSpec) (StageExecution, error) {
				return StageExecution{
					Status: StatusEvidenceReady, EvidenceClass: EvidenceReal,
					Artifacts: []ArtifactRef{
						{Slot: "charter", ID: artifactID, Digest: repeatHex('a'), Source: "focused-test-executor"},
						{Slot: "context_manifest", ID: "ctxm_" + strings.Repeat("b", 64), Digest: repeatHex('b'), Source: "focused-test-executor"},
					},
					Assertions: []Assertion{{ID: "stage-contract", Passed: true}},
				}, nil
			})
			result, err := (Runner{
				Scenario: scenario, Mode: ModeReal, Capabilities: allCapabilities(), Executors: executors,
			}).Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusFail {
				t.Fatalf("overall status = %q, want FAIL", result.Status)
			}
			for _, stage := range result.Stages {
				if stage.ID != StageCharterContext {
					continue
				}
				if stage.Status != StatusFail || stage.Blocker != "INVALID_ARTIFACT:charter" {
					t.Fatalf("charter stage = %+v, want INVALID_ARTIFACT:charter", stage)
				}
				return
			}
			t.Fatal("charter stage result missing")
		})
	}
}

func TestRunnerResultIsDeterministicAndKeepsArtifactSlots(t *testing.T) {
	scenario, err := LoadScenario()
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Scenario:     scenario,
		Mode:         ModeReal,
		Capabilities: allCapabilities(),
		Executors:    allExecutors(scenario, EvidenceReal),
	}
	first, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("same runner inputs produced different JSON")
	}
	if !reflect.DeepEqual(first.ArtifactSlots, scenario.ArtifactSlots) {
		t.Fatal("result lost the exact artifact slot list")
	}
	if len(first.Trace) == 0 || first.Trace[0].Sequence != 1 {
		t.Fatalf("trace = %+v", first.Trace)
	}
}
