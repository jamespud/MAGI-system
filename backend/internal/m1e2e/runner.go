package m1e2e

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const VerdictNotEvaluated = "NOT_EVALUATED"

var ErrPassM1Forbidden = errors.New("m1e2e: stage executors may not award PASS_M1")

type Capabilities map[Capability]bool

type ArtifactRef struct {
	Slot   string `json:"slot"`
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Source string `json:"source"`
}

type Assertion struct {
	ID     string `json:"id"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type StageExecution struct {
	Status        Status        `json:"status"`
	EvidenceClass EvidenceClass `json:"evidence_class"`
	Artifacts     []ArtifactRef `json:"artifacts,omitempty"`
	Assertions    []Assertion   `json:"assertions,omitempty"`
}

type StageExecutor interface {
	Execute(context.Context, Scenario, StageSpec) (StageExecution, error)
}

type Runner struct {
	Scenario     Scenario
	Mode         Mode
	Capabilities Capabilities
	Executors    map[StageID]StageExecutor
}

type StageResult struct {
	ID                StageID       `json:"id"`
	Name              string        `json:"name"`
	Status            Status        `json:"status"`
	EvidenceClass     EvidenceClass `json:"evidence_class"`
	Blocker           string        `json:"blocker,omitempty"`
	Note              string        `json:"note,omitempty"`
	RequiredArtifacts []string      `json:"required_artifacts"`
	Artifacts         []ArtifactRef `json:"artifacts,omitempty"`
	Assertions        []Assertion   `json:"assertions,omitempty"`
}

type NegativeCaseResult struct {
	ID              string              `json:"id"`
	Stage           StageID             `json:"stage"`
	Status          Status              `json:"status"`
	ExpectedStatus  Status              `json:"expected_status"`
	ExpectedBlocker string              `json:"expected_blocker"`
	Requirement     NegativeRequirement `json:"requirement"`
	Note            string              `json:"note"`
}

type TraceEvent struct {
	Sequence int     `json:"sequence"`
	Stage    StageID `json:"stage"`
	Event    string  `json:"event"`
	Detail   string  `json:"detail,omitempty"`
}

type RunResult struct {
	ScenarioID      string               `json:"scenario_id"`
	ScenarioVersion string               `json:"scenario_version"`
	RunnerVersion   string               `json:"runner_version"`
	Mode            Mode                 `json:"mode"`
	Status          Status               `json:"status"`
	M1Verdict       string               `json:"m1_verdict"`
	ArtifactSlots   []ArtifactSlot       `json:"artifact_slots"`
	Stages          []StageResult        `json:"stages"`
	NegativeCases   []NegativeCaseResult `json:"negative_cases"`
	Trace           []TraceEvent         `json:"trace"`
}

func (r Runner) Run(ctx context.Context) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, errors.New("m1e2e: nil context")
	}
	if r.Mode != ModeReal && r.Mode != ModeSynthetic {
		return RunResult{}, fmt.Errorf("m1e2e: unsupported mode %q", r.Mode)
	}
	if err := r.Scenario.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("m1e2e: invalid scenario: %w", err)
	}

	result := RunResult{
		ScenarioID:      r.Scenario.ScenarioID,
		ScenarioVersion: r.Scenario.Version,
		RunnerVersion:   RunnerVersion,
		Mode:            r.Mode,
		Status:          StatusNotRun,
		M1Verdict:       VerdictNotEvaluated,
		ArtifactSlots:   append([]ArtifactSlot(nil), r.Scenario.ArtifactSlots...),
	}
	for _, negative := range r.Scenario.NegativeCases {
		result.NegativeCases = append(result.NegativeCases, NegativeCaseResult{
			ID: negative.ID, Stage: negative.Stage, Status: StatusNotRun,
			ExpectedStatus: negative.ExpectedStatus, ExpectedBlocker: negative.ExpectedBlocker,
			Requirement: negative.Requirement,
			Note:        "definition only; M1-00 does not execute business negative cases",
		})
	}

	for _, stage := range r.Scenario.Stages {
		stageResult, trace, err := r.runStage(ctx, stage)
		if err != nil {
			return RunResult{}, err
		}
		result.Stages = append(result.Stages, stageResult)
		result.Trace = append(result.Trace, trace...)
	}
	result.Status = aggregateStatus(result.Stages, r.Mode)
	for i := range result.Trace {
		result.Trace[i].Sequence = i + 1
	}
	return result, nil
}

func (r Runner) runStage(ctx context.Context, stage StageSpec) (StageResult, []TraceEvent, error) {
	result := StageResult{
		ID: stage.ID, Name: stage.Name, Status: StatusNotRun,
		EvidenceClass: EvidenceNone, RequiredArtifacts: append([]string(nil), stage.RequiredArtifacts...),
	}
	trace := []TraceEvent{{Stage: stage.ID, Event: "stage_started"}}

	if stage.ID == StageM1Gate {
		result.Note = "independent M1 gate is a separate acceptance boundary"
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_not_run", Detail: result.Note})
		return result, trace, nil
	}

	// Scenario validation is the one built-in M1-00 stage. It never stands in
	// for a business capability.
	if stage.ID == StageScenarioContract {
		result.Status = StatusEvidenceReady
		result.EvidenceClass = EvidenceReal
		result.Assertions = []Assertion{{ID: "fixed-scenario-contract", Passed: true, Detail: "embedded scenario validated"}}
		if r.Mode == ModeSynthetic {
			result.Status = StatusDiagnosticOnly
			result.EvidenceClass = EvidenceSynthetic
		}
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_result", Detail: string(result.Status)})
		return result, trace, nil
	}

	for _, capability := range stage.RequiredCapabilities {
		if !r.Capabilities[capability] {
			result.Status = StatusBlocked
			result.Blocker = "CAPABILITY_UNAVAILABLE:" + string(capability)
			trace = append(trace, TraceEvent{Stage: stage.ID, Event: "capability_blocked", Detail: string(capability)})
			return result, trace, nil
		}
	}

	executor := r.Executors[stage.ID]
	if executor == nil {
		result.Status = StatusBlocked
		result.Blocker = "STAGE_EXECUTOR_UNAVAILABLE"
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_blocked", Detail: result.Blocker})
		return result, trace, nil
	}

	execution, err := executor.Execute(ctx, r.Scenario, stage)
	if err != nil {
		result.Status = StatusFail
		result.Blocker = "STAGE_EXECUTOR_ERROR"
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_failed", Detail: err.Error()})
		return result, trace, nil
	}
	if execution.Status == StatusPassM1 {
		return StageResult{}, nil, ErrPassM1Forbidden
	}

	result.EvidenceClass = execution.EvidenceClass
	result.Artifacts = append([]ArtifactRef(nil), execution.Artifacts...)
	result.Assertions = append([]Assertion(nil), execution.Assertions...)

	switch execution.Status {
	case StatusBlocked:
		result.Status = StatusBlocked
		result.Blocker = "STAGE_EXECUTOR_BLOCKED"
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_blocked", Detail: result.Blocker})
		return result, trace, nil
	case StatusNotRun:
		result.Status = StatusNotRun
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_not_run"})
		return result, trace, nil
	case StatusFail:
		result.Status = StatusFail
		result.Blocker = "STAGE_EXECUTOR_FAILED"
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_failed", Detail: result.Blocker})
		return result, trace, nil
	case StatusDiagnosticOnly:
		if r.Mode == ModeReal {
			result.Status = StatusFail
			result.Blocker = "DIAGNOSTIC_ONLY_IN_REAL_MODE"
			trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_failed", Detail: result.Blocker})
			return result, trace, nil
		}
		result.Status = StatusDiagnosticOnly
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_result", Detail: string(result.Status)})
		return result, trace, nil
	case StatusEvidenceReady:
		// Continue to evidence classification.
	default:
		result.Status = StatusFail
		result.Blocker = "INVALID_STAGE_STATUS:" + string(execution.Status)
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_failed", Detail: result.Blocker})
		return result, trace, nil
	}

	if blocker := r.classifyEvidence(stage, &result); blocker != "" {
		result.Status = StatusFail
		result.Blocker = blocker
		trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_failed", Detail: blocker})
		return result, trace, nil
	}

	trace = append(trace, TraceEvent{Stage: stage.ID, Event: "stage_result", Detail: string(result.Status)})
	return result, trace, nil
}

func (r Runner) classifyEvidence(stage StageSpec, result *StageResult) string {
	if result.EvidenceClass == EvidenceSynthetic && r.Mode == ModeReal {
		return "SYNTHETIC_EVIDENCE_IN_REAL_MODE"
	}
	if r.Mode == ModeReal && result.EvidenceClass != EvidenceReal {
		return "NON_REAL_EVIDENCE_IN_REAL_MODE"
	}
	if blocker := validateArtifacts(r.Scenario, stage, result.Artifacts); blocker != "" {
		return blocker
	}
	for _, assertion := range result.Assertions {
		if assertion.ID == "" || !assertion.Passed {
			return "ASSERTION_FAILED:" + assertion.ID
		}
	}

	if r.Mode == ModeSynthetic {
		result.Status = StatusDiagnosticOnly
		return ""
	}
	result.Status = StatusEvidenceReady
	return ""
}

func validateArtifacts(scenario Scenario, stage StageSpec, artifacts []ArtifactRef) string {
	required := make(map[string]struct{}, len(stage.RequiredArtifacts))
	for _, slot := range stage.RequiredArtifacts {
		required[slot] = struct{}{}
	}
	slots := make(map[string]ArtifactSlot, len(scenario.ArtifactSlots))
	for _, slot := range scenario.ArtifactSlots {
		slots[slot.Name] = slot
	}
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if _, ok := required[artifact.Slot]; !ok {
			return "UNDECLARED_ARTIFACT:" + artifact.Slot
		}
		slot, ok := slots[artifact.Slot]
		if !ok || slot.Stage != stage.ID || !artifactIDRE.MatchString(artifact.ID) || !strings.HasPrefix(artifact.ID, slot.IDPrefix+"_") ||
			artifact.Source == "" || !digestRE.MatchString(artifact.Digest) {
			return "INVALID_ARTIFACT:" + artifact.Slot
		}
		if _, exists := seen[artifact.Slot]; exists {
			return "DUPLICATE_ARTIFACT:" + artifact.Slot
		}
		seen[artifact.Slot] = struct{}{}
	}
	for _, slot := range stage.RequiredArtifacts {
		if _, ok := seen[slot]; !ok {
			return "MISSING_REQUIRED_ARTIFACT:" + slot
		}
	}
	return ""
}

func aggregateStatus(stages []StageResult, mode Mode) Status {
	statuses := make([]Status, 0, len(stages))
	for _, stage := range stages {
		if stage.ID == StageM1Gate {
			continue
		}
		statuses = append(statuses, stage.Status)
	}
	if slices.Contains(statuses, StatusFail) {
		return StatusFail
	}
	if slices.Contains(statuses, StatusBlocked) {
		return StatusBlocked
	}
	if mode == ModeSynthetic || slices.Contains(statuses, StatusDiagnosticOnly) {
		return StatusDiagnosticOnly
	}
	if len(statuses) == 0 || slices.Contains(statuses, StatusNotRun) {
		return StatusNotRun
	}
	for _, status := range statuses {
		if status != StatusEvidenceReady {
			return StatusNotRun
		}
	}
	return StatusEvidenceReady
}

func (s Status) IsTerminal() bool {
	switch s {
	case StatusDiagnosticOnly, StatusFail, StatusEvidenceReady, StatusPassM1:
		return true
	default:
		return false
	}
}

func (r RunResult) Blockers() []string {
	var blockers []string
	for _, stage := range r.Stages {
		if stage.Blocker != "" {
			blockers = append(blockers, string(stage.ID)+":"+stage.Blocker)
		}
	}
	slices.Sort(blockers)
	return blockers
}

func (r RunResult) String() string {
	return fmt.Sprintf("%s/%s status=%s verdict=%s blockers=%s", r.ScenarioID, r.Mode, r.Status, r.M1Verdict, strings.Join(r.Blockers(), ","))
}
