// Package m1e2e defines the evidence-only ROLLOUT-LAB-E2E-01 harness.
//
// It intentionally contains no Persona, Journal, authority, Provider, closure,
// ballot, decision, report or Egress implementation. It can produce evidence
// readiness, but it can never award PASS_M1.
package m1e2e

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
)

const (
	ScenarioID      = "ROLLOUT-LAB-E2E-01"
	ScenarioVersion = "1.0"
	RunnerVersion   = "m1e2e/1"
)

type Mode string

const (
	ModeReal      Mode = "REAL"
	ModeSynthetic Mode = "SYNTHETIC"
)

type Status string

const (
	StatusNotRun         Status = "NOT_RUN"
	StatusBlocked        Status = "BLOCKED"
	StatusDiagnosticOnly Status = "DIAGNOSTIC_ONLY"
	StatusFail           Status = "FAIL"
	StatusEvidenceReady  Status = "EVIDENCE_READY"
	// StatusPassM1 exists only so the harness can reject it explicitly. No
	// M1-00 component is allowed to return it.
	StatusPassM1 Status = "PASS_M1"
)

type EvidenceClass string

const (
	EvidenceNone      EvidenceClass = "NONE"
	EvidenceSynthetic EvidenceClass = "SYNTHETIC"
	EvidenceReal      EvidenceClass = "REAL"
)

type StageID string

const (
	StageScenarioContract     StageID = "SCENARIO_CONTRACT"
	StageCharterContext       StageID = "CHARTER_CONTEXT"
	StageJournalAdmission     StageID = "JOURNAL_ADMISSION"
	StagePersonaInvestigation StageID = "PERSONA_INVESTIGATION"
	StageEvidenceClosure      StageID = "EVIDENCE_CLOSURE"
	StageSealedBallots        StageID = "SEALED_BALLOTS"
	StageDecisionRecord       StageID = "DECISION_RECORD"
	StageTrustedReport        StageID = "TRUSTED_REPORT"
	StageR0EgressCheck        StageID = "R0_EGRESS_CHECK"
	StageM1Gate               StageID = "M1_GATE"
)

type Capability string

const (
	CapabilityScenarioFixture  Capability = "scenario_fixture"
	CapabilityCharterContext   Capability = "charter_context"
	CapabilityJournalAdmission Capability = "journal_admission_v2"
	CapabilityProviderDelivery Capability = "provider_delivery"
	CapabilityPersonaRuntime   Capability = "persona_runtime"
	CapabilityEvidenceClosure  Capability = "evidence_closure"
	CapabilitySealedBallots    Capability = "sealed_ballots"
	CapabilityDecisionRecord   Capability = "decision_record"
	CapabilityTrustedReport    Capability = "trusted_report"
	CapabilityR0EgressProbe    Capability = "r0_egress_probe"
)

type Goal struct {
	Statement       string   `json:"statement"`
	HardConstraints []string `json:"hard_constraints"`
	PreferenceOrder []string `json:"preference_order"`
}

type Subject struct {
	SubjectID string `json:"subject_id"`
	Role      string `json:"role"`
}

type PersonaSpec struct {
	PersonaID             string   `json:"persona_id"`
	SpecVersion           string   `json:"spec_version"`
	Mission               string   `json:"mission"`
	MaxToolCalls          uint64   `json:"max_tool_calls"`
	MaxOutputTokens       uint64   `json:"max_output_tokens"`
	MinimumInvestigations []string `json:"minimum_investigations"`
}

type Candidate struct {
	CandidateID            string   `json:"candidate_id"`
	Proposal               string   `json:"proposal"`
	RiskLevel              string   `json:"risk_level"`
	IsNoAction             bool     `json:"is_no_action"`
	RequiresProtectedWrite bool     `json:"requires_protected_write"`
	Conditions             []string `json:"conditions"`
}

type Material struct {
	MaterialID    string `json:"material_id"`
	Kind          string `json:"kind"`
	SourceRef     string `json:"source_ref"`
	Digest        string `json:"digest"`
	ValidUntil    string `json:"valid_until"`
	SyntheticOnly bool   `json:"synthetic_only"`
}

type ArtifactSlot struct {
	Name         string  `json:"name"`
	ArtifactType string  `json:"artifact_type"`
	IDPrefix     string  `json:"id_prefix"`
	PersonaID    string  `json:"persona_id,omitempty"`
	Stage        StageID `json:"stage"`
}

type StageSpec struct {
	ID                   StageID      `json:"id"`
	Name                 string       `json:"name"`
	RequiredCapabilities []Capability `json:"required_capabilities"`
	RequiredArtifacts    []string     `json:"required_artifacts"`
}

type NegativeRequirement string

const (
	NegativeRequirementRequired     NegativeRequirement = "REQUIRED"
	NegativeRequirementSupplemental NegativeRequirement = "SUPPLEMENTAL"
)

type NegativeCase struct {
	ID              string              `json:"id"`
	Stage           StageID             `json:"stage"`
	ExpectedStatus  Status              `json:"expected_status"`
	ExpectedBlocker string              `json:"expected_blocker"`
	Requirement     NegativeRequirement `json:"requirement"`
}

type Scenario struct {
	ScenarioID               string         `json:"scenario_id"`
	Version                  string         `json:"version"`
	CaseID                   string         `json:"case_id"`
	Round                    uint64         `json:"round"`
	ExecutionGeneration      uint64         `json:"execution_generation"`
	RiskLevel                string         `json:"risk_level"`
	ProtectedWritesPermitted bool           `json:"protected_writes_permitted"`
	Goal                     Goal           `json:"goal"`
	Subjects                 []Subject      `json:"subjects"`
	Personas                 []PersonaSpec  `json:"personas"`
	Candidates               []Candidate    `json:"candidates"`
	Materials                []Material     `json:"materials"`
	ArtifactSlots            []ArtifactSlot `json:"artifact_slots"`
	Stages                   []StageSpec    `json:"stages"`
	NegativeCases            []NegativeCase `json:"negative_cases"`
}

//go:embed testdata/rollout_lab_e2e_01.json
var embeddedScenario []byte

var (
	digestRE              = regexp.MustCompile(`^[0-9a-f]{64}$`)
	artifactIDRE          = regexp.MustCompile(`^[A-Za-z0-9]+_[0-9a-f]{64}$`)
	knownArtifactPrefixes = map[string]string{
		"decision_charter":         "chtr",
		"context_manifest":         "ctxm",
		"candidate_snapshot":       "csnp",
		"journal_admission":        "jadm",
		"trusted_delivery_receipt": "drcp",
		"read_set_snapshot":        "rset",
		"candidate_confirmation":   "cnfm",
		"evidence_closure":         "ecls",
		"sealed_ballot":            "sbll",
		"ballot_set":               "bset",
		"decision_record":          "drec",
		"trusted_report":           "rpt",
	}
	expectedGoal = Goal{
		Statement: "Within the allowed error rate and operational burden, prefer at least a 10% P95 request-latency improvement while preserving stop authority and verifiable recovery.",
		HardConstraints: []string{
			"Preserve Requester stop authority.",
			"Recovery must be independently verifiable.",
			"Do not expand authorization.",
			"R0 must not obtain or use a protected write capability.",
		},
		PreferenceOrder: []string{"lower verified risk", "lower operational burden", "stronger verified effect"},
	}
	expectedCandidates = []Candidate{
		{CandidateID: "C0", Proposal: "Keep the current state and continue observing.", RiskLevel: "R0", IsNoAction: true, RequiresProtectedWrite: false, Conditions: []string{}},
		{CandidateID: "C1", Proposal: "Run a shadow validation without routing real requests or changing the running version.", RiskLevel: "R0", IsNoAction: false, RequiresProtectedWrite: false, Conditions: []string{"Do not route production traffic", "Do not change the running version"}},
		{CandidateID: "C2", Proposal: "Run an isolated 5% canary for at most 15 minutes with bounded error and recovery limits.", RiskLevel: "R1", IsNoAction: false, RequiresProtectedWrite: true, Conditions: []string{"Additional error rate <= 0.5 percentage points", "Verified recovery <= 60 seconds", "Separate authorization required"}},
		{CandidateID: "C3", Proposal: "Run an isolated 10% canary for at most 15 minutes with the same limits and per-attempt human approval.", RiskLevel: "R2", IsNoAction: false, RequiresProtectedWrite: true, Conditions: []string{"Additional error rate <= 0.5 percentage points", "Verified recovery <= 60 seconds", "Per-attempt human approval"}},
	}
	expectedArtifactSlots = []ArtifactSlot{
		{Name: "charter", ArtifactType: "decision_charter", IDPrefix: "chtr", Stage: StageCharterContext},
		{Name: "context_manifest", ArtifactType: "context_manifest", IDPrefix: "ctxm", Stage: StageCharterContext},
		{Name: "journal_admission_melchior", ArtifactType: "journal_admission", IDPrefix: "jadm", PersonaID: "melchior", Stage: StageJournalAdmission},
		{Name: "journal_admission_balthasar", ArtifactType: "journal_admission", IDPrefix: "jadm", PersonaID: "balthasar", Stage: StageJournalAdmission},
		{Name: "journal_admission_casper", ArtifactType: "journal_admission", IDPrefix: "jadm", PersonaID: "casper", Stage: StageJournalAdmission},
		{Name: "delivery_receipt_melchior", ArtifactType: "trusted_delivery_receipt", IDPrefix: "drcp", PersonaID: "melchior", Stage: StageJournalAdmission},
		{Name: "delivery_receipt_balthasar", ArtifactType: "trusted_delivery_receipt", IDPrefix: "drcp", PersonaID: "balthasar", Stage: StageJournalAdmission},
		{Name: "delivery_receipt_casper", ArtifactType: "trusted_delivery_receipt", IDPrefix: "drcp", PersonaID: "casper", Stage: StageJournalAdmission},
		{Name: "candidate_snapshot", ArtifactType: "candidate_snapshot", IDPrefix: "csnp", Stage: StagePersonaInvestigation},
		{Name: "readset_melchior", ArtifactType: "read_set_snapshot", IDPrefix: "rset", PersonaID: "melchior", Stage: StageEvidenceClosure},
		{Name: "readset_balthasar", ArtifactType: "read_set_snapshot", IDPrefix: "rset", PersonaID: "balthasar", Stage: StageEvidenceClosure},
		{Name: "readset_casper", ArtifactType: "read_set_snapshot", IDPrefix: "rset", PersonaID: "casper", Stage: StageEvidenceClosure},
		{Name: "confirmation_melchior", ArtifactType: "candidate_confirmation", IDPrefix: "cnfm", PersonaID: "melchior", Stage: StageEvidenceClosure},
		{Name: "confirmation_balthasar", ArtifactType: "candidate_confirmation", IDPrefix: "cnfm", PersonaID: "balthasar", Stage: StageEvidenceClosure},
		{Name: "confirmation_casper", ArtifactType: "candidate_confirmation", IDPrefix: "cnfm", PersonaID: "casper", Stage: StageEvidenceClosure},
		{Name: "evidence_closure", ArtifactType: "evidence_closure", IDPrefix: "ecls", Stage: StageEvidenceClosure},
		{Name: "sealed_ballot_melchior", ArtifactType: "sealed_ballot", IDPrefix: "sbll", PersonaID: "melchior", Stage: StageSealedBallots},
		{Name: "sealed_ballot_balthasar", ArtifactType: "sealed_ballot", IDPrefix: "sbll", PersonaID: "balthasar", Stage: StageSealedBallots},
		{Name: "sealed_ballot_casper", ArtifactType: "sealed_ballot", IDPrefix: "sbll", PersonaID: "casper", Stage: StageSealedBallots},
		{Name: "ballot_set", ArtifactType: "ballot_set", IDPrefix: "bset", Stage: StageDecisionRecord},
		{Name: "decision_record", ArtifactType: "decision_record", IDPrefix: "drec", Stage: StageDecisionRecord},
		{Name: "trusted_report", ArtifactType: "trusted_report", IDPrefix: "rpt", Stage: StageTrustedReport},
	}
)

var expectedStages = []struct {
	id        StageID
	caps      []Capability
	artifacts []string
}{
	{StageScenarioContract, []Capability{CapabilityScenarioFixture}, []string{}},
	{StageCharterContext, []Capability{CapabilityCharterContext}, []string{"charter", "context_manifest"}},
	{StageJournalAdmission, []Capability{CapabilityJournalAdmission, CapabilityProviderDelivery}, []string{
		"journal_admission_melchior", "journal_admission_balthasar", "journal_admission_casper",
		"delivery_receipt_melchior", "delivery_receipt_balthasar", "delivery_receipt_casper",
	}},
	{StagePersonaInvestigation, []Capability{CapabilityPersonaRuntime}, []string{"candidate_snapshot"}},
	{StageEvidenceClosure, []Capability{CapabilityEvidenceClosure}, []string{
		"readset_melchior", "readset_balthasar", "readset_casper",
		"confirmation_melchior", "confirmation_balthasar", "confirmation_casper", "evidence_closure",
	}},
	{StageSealedBallots, []Capability{CapabilitySealedBallots}, []string{
		"sealed_ballot_melchior", "sealed_ballot_balthasar", "sealed_ballot_casper",
	}},
	{StageDecisionRecord, []Capability{CapabilityDecisionRecord}, []string{"ballot_set", "decision_record"}},
	{StageTrustedReport, []Capability{CapabilityTrustedReport}, []string{"trusted_report"}},
	{StageR0EgressCheck, []Capability{CapabilityR0EgressProbe}, []string{}},
	{StageM1Gate, nil, []string{}},
}

var requiredNegativeCases = []struct {
	id      string
	stage   StageID
	status  Status
	blocker string
}{
	{"missing_third_receipt", StageEvidenceClosure, StatusBlocked, "THIRD_RECEIPT_MISSING"},
	{"stale_material", StageJournalAdmission, StatusBlocked, "MATERIAL_STALE"},
	{"revoked_context", StageJournalAdmission, StatusBlocked, "CONTEXT_REVOKED"},
	{"duplicate_ballot", StageSealedBallots, StatusFail, "DUPLICATE_OR_CONFLICTING_BALLOT"},
	{"witness_unavailable", StageM1Gate, StatusBlocked, "WITNESS_UNAVAILABLE"},
	{"provider_outcome_unknown", StageJournalAdmission, StatusBlocked, "PROVIDER_OUTCOME_UNKNOWN"},
}

var supplementalNegativeCases = []struct {
	id      string
	stage   StageID
	status  Status
	blocker string
}{
	{"old_generation_replay", StageJournalAdmission, StatusBlocked, "OLD_GENERATION_REPLAY"},
	{"protected_send_attempt_r0", StageR0EgressCheck, StatusFail, "PROTECTED_WRITE_ATTEMPTED_IN_R0"},
}

var requiredRoles = []string{"Requester", "ServiceOwner", "ChangeApprover", "IndependentVerifier", "Arbitrator", "BackupArbitrator"}

// LoadScenario returns the single embedded M1 reference scenario. Callers must
// not be able to supply a different REAL-mode scenario through the harness.
func LoadScenario() (Scenario, error) {
	decoder := json.NewDecoder(bytes.NewReader(embeddedScenario))
	decoder.DisallowUnknownFields()
	var scenario Scenario
	if err := decoder.Decode(&scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode embedded scenario: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Scenario{}, errors.New("embedded scenario contains more than one JSON value")
		}
		return Scenario{}, fmt.Errorf("decode embedded scenario trailer: %w", err)
	}
	if err := scenario.Validate(); err != nil {
		return Scenario{}, err
	}
	return scenario, nil
}

// Validate enforces the fixed M1 scenario contract. It does not inspect or
// trust any runtime success flag.
func (s Scenario) Validate() error {
	if s.ScenarioID != ScenarioID || s.Version != ScenarioVersion {
		return fmt.Errorf("scenario identity = %q/%q, want %q/%q", s.ScenarioID, s.Version, ScenarioID, ScenarioVersion)
	}
	if s.CaseID == "" || s.Round == 0 || s.ExecutionGeneration == 0 {
		return errors.New("scenario case/round/generation must be explicit and non-zero")
	}
	if s.RiskLevel != "R0" || s.ProtectedWritesPermitted {
		return errors.New("M1 reference scenario must be R0 and prohibit protected writes")
	}
	if !reflect.DeepEqual(s.Goal, expectedGoal) {
		return errors.New("goal differs from the frozen ROLLOUT-LAB-E2E-01 objective")
	}
	if err := validateSubjects(s.Subjects); err != nil {
		return err
	}
	if err := validatePersonas(s.Personas); err != nil {
		return err
	}
	if !reflect.DeepEqual(s.Candidates, expectedCandidates) {
		return errors.New("candidates differ from the frozen C0-C3 inventory")
	}
	if err := validateCandidates(s.Candidates); err != nil {
		return err
	}
	if err := validateMaterials(s.Materials); err != nil {
		return err
	}
	if err := validateArtifactSlots(s.ArtifactSlots); err != nil {
		return err
	}
	if err := validateStages(s.Stages, s.ArtifactSlots); err != nil {
		return err
	}
	return validateNegativeCases(s.NegativeCases, s.Stages)
}

func validateSubjects(subjects []Subject) error {
	if len(subjects) == 0 {
		return errors.New("subjects are required")
	}
	seen := make(map[string]struct{}, len(subjects))
	roles := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		if subject.SubjectID == "" || subject.Role == "" {
			return errors.New("subject ID and role are required")
		}
		if _, exists := seen[subject.SubjectID]; exists {
			return fmt.Errorf("duplicate subject %q", subject.SubjectID)
		}
		seen[subject.SubjectID] = struct{}{}
		roles[subject.Role] = struct{}{}
	}
	for _, role := range requiredRoles {
		if _, ok := roles[role]; !ok {
			return fmt.Errorf("required subject role %q is missing", role)
		}
	}
	return nil
}

func validatePersonas(personas []PersonaSpec) error {
	want := []string{"melchior", "balthasar", "casper"}
	if len(personas) != len(want) {
		return fmt.Errorf("persona count = %d, want %d", len(personas), len(want))
	}
	for i, persona := range personas {
		if persona.PersonaID != want[i] {
			return fmt.Errorf("persona[%d] = %q, want %q", i, persona.PersonaID, want[i])
		}
		if persona.SpecVersion != "1.0" || persona.Mission == "" {
			return fmt.Errorf("persona %s must pin spec 1.0 and mission", persona.PersonaID)
		}
		if persona.MaxToolCalls != 6 || persona.MaxOutputTokens != 6000 {
			return fmt.Errorf("persona %s budget = %d/%d, want 6/6000", persona.PersonaID, persona.MaxToolCalls, persona.MaxOutputTokens)
		}
		if len(persona.MinimumInvestigations) < 2 {
			return fmt.Errorf("persona %s must declare at least two minimum investigations", persona.PersonaID)
		}
	}
	return nil
}

func validateCandidates(candidates []Candidate) error {
	want := []string{"C0", "C1", "C2", "C3"}
	if len(candidates) != len(want) {
		return fmt.Errorf("candidate count = %d, want %d", len(candidates), len(want))
	}
	seen := make(map[string]struct{}, len(candidates))
	noAction := 0
	for i, candidate := range candidates {
		if candidate.CandidateID != want[i] {
			return fmt.Errorf("candidate[%d] = %q, want %q", i, candidate.CandidateID, want[i])
		}
		if candidate.Proposal == "" || candidate.RiskLevel == "" {
			return fmt.Errorf("candidate %s is missing proposal or risk", candidate.CandidateID)
		}
		if _, exists := seen[candidate.CandidateID]; exists {
			return fmt.Errorf("duplicate candidate %q", candidate.CandidateID)
		}
		seen[candidate.CandidateID] = struct{}{}
		if candidate.IsNoAction {
			noAction++
		}
		switch candidate.CandidateID {
		case "C0", "C1":
			if candidate.RiskLevel != "R0" || candidate.RequiresProtectedWrite {
				return fmt.Errorf("candidate %s must be R0 without protected write", candidate.CandidateID)
			}
		case "C2", "C3":
			if !candidate.RequiresProtectedWrite {
				return fmt.Errorf("candidate %s must remain analysis-only unless separately authorized", candidate.CandidateID)
			}
		}
	}
	if noAction != 1 || !candidates[0].IsNoAction {
		return errors.New("C0 must be the unique no-action candidate")
	}
	return nil
}

func validateMaterials(materials []Material) error {
	if len(materials) == 0 {
		return errors.New("controlled materials are required")
	}
	seen := make(map[string]struct{}, len(materials))
	for _, material := range materials {
		if material.MaterialID == "" || material.Kind == "" || material.SourceRef == "" || material.ValidUntil == "" {
			return fmt.Errorf("material %q is incomplete", material.MaterialID)
		}
		if !digestRE.MatchString(material.Digest) {
			return fmt.Errorf("material %q has invalid digest", material.MaterialID)
		}
		if _, exists := seen[material.MaterialID]; exists {
			return fmt.Errorf("duplicate material %q", material.MaterialID)
		}
		seen[material.MaterialID] = struct{}{}
	}
	return nil
}

func validateArtifactSlots(slots []ArtifactSlot) error {
	if !reflect.DeepEqual(slots, expectedArtifactSlots) {
		return fmt.Errorf("artifact slot inventory differs from the frozen %d-slot contract", len(expectedArtifactSlots))
	}
	seen := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		if slot.Name == "" || slot.Stage == "" {
			return errors.New("artifact slot name and stage are required")
		}
		if _, exists := seen[slot.Name]; exists {
			return fmt.Errorf("duplicate artifact slot %q", slot.Name)
		}
		seen[slot.Name] = struct{}{}
		wantPrefix, ok := knownArtifactPrefixes[slot.ArtifactType]
		if !ok || slot.IDPrefix != wantPrefix {
			return fmt.Errorf("artifact slot %q has unsupported type/prefix %q/%q", slot.Name, slot.ArtifactType, slot.IDPrefix)
		}
		if slot.PersonaID != "" && !slices.Contains([]string{"melchior", "balthasar", "casper"}, slot.PersonaID) {
			return fmt.Errorf("artifact slot %q has unknown persona %q", slot.Name, slot.PersonaID)
		}
	}
	return nil
}

func validateStages(stages []StageSpec, slots []ArtifactSlot) error {
	if len(stages) != len(expectedStages) {
		return fmt.Errorf("stage count = %d, want %d", len(stages), len(expectedStages))
	}
	slotsByStage := make(map[StageID]map[string]struct{})
	for _, slot := range slots {
		if slotsByStage[slot.Stage] == nil {
			slotsByStage[slot.Stage] = make(map[string]struct{})
		}
		slotsByStage[slot.Stage][slot.Name] = struct{}{}
	}
	for i, want := range expectedStages {
		stage := stages[i]
		if stage.ID != want.id || stage.Name == "" {
			return fmt.Errorf("stage[%d] = %q, want %q with a name", i, stage.ID, want.id)
		}
		if !slices.Equal(stage.RequiredCapabilities, want.caps) {
			return fmt.Errorf("stage %s capabilities = %v, want %v", stage.ID, stage.RequiredCapabilities, want.caps)
		}
		if !slices.Equal(stage.RequiredArtifacts, want.artifacts) {
			return fmt.Errorf("stage %s artifacts = %v, want %v", stage.ID, stage.RequiredArtifacts, want.artifacts)
		}
		seenArtifacts := make(map[string]struct{}, len(stage.RequiredArtifacts))
		for _, artifact := range stage.RequiredArtifacts {
			if _, exists := seenArtifacts[artifact]; exists {
				return fmt.Errorf("stage %s repeats artifact %q", stage.ID, artifact)
			}
			seenArtifacts[artifact] = struct{}{}
			if _, ok := slotsByStage[stage.ID][artifact]; !ok {
				return fmt.Errorf("stage %s requires artifact %q outside its slot stage", stage.ID, artifact)
			}
		}
		if stage.ID == StageM1Gate && len(stage.RequiredArtifacts) != 0 {
			return errors.New("independent M1 gate cannot consume harness-owned artifact slots")
		}
	}
	return nil
}

func validateNegativeCases(cases []NegativeCase, stages []StageSpec) error {
	wantCount := len(requiredNegativeCases) + len(supplementalNegativeCases)
	if len(cases) != wantCount {
		return fmt.Errorf("negative case count = %d, want %d", len(cases), wantCount)
	}
	stageIDs := make(map[StageID]struct{}, len(stages))
	for _, stage := range stages {
		stageIDs[stage.ID] = struct{}{}
	}
	for i, want := range requiredNegativeCases {
		negative := cases[i]
		if negative.ID != want.id || negative.Stage != want.stage || negative.ExpectedStatus != want.status || negative.ExpectedBlocker != want.blocker || negative.Requirement != NegativeRequirementRequired {
			return fmt.Errorf("required negative case[%d] = %+v, want %s/%s/%s/%s/REQUIRED", i, negative, want.id, want.stage, want.status, want.blocker)
		}
		if _, ok := stageIDs[negative.Stage]; !ok {
			return fmt.Errorf("negative case %s references unknown stage %s", negative.ID, negative.Stage)
		}
	}
	for i, want := range supplementalNegativeCases {
		negative := cases[len(requiredNegativeCases)+i]
		if negative.ID != want.id || negative.Stage != want.stage || negative.ExpectedStatus != want.status || negative.ExpectedBlocker != want.blocker || negative.Requirement != NegativeRequirementSupplemental {
			return fmt.Errorf("supplemental negative case[%d] = %+v, want %s/%s/%s/%s/SUPPLEMENTAL", i, negative, want.id, want.stage, want.status, want.blocker)
		}
		if _, ok := stageIDs[negative.Stage]; !ok {
			return fmt.Errorf("negative case %s references unknown stage %s", negative.ID, negative.Stage)
		}
	}
	return nil
}
