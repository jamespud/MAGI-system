// Package inputartifact validates input artifacts offline. It cannot publish or authorize them.
package inputartifact

import "encoding/json"

type ExternalRef struct {
	Namespace     string `json:"namespace"`
	ObjectID      string `json:"object_id"`
	Version       string `json:"version"`
	DigestAlg     string `json:"digest_alg"`
	ContentDigest string `json:"content_digest"`
}

type SubjectRef struct {
	SubjectID             string `json:"subject_id"`
	IdentitySourceID      string `json:"identity_source_id"`
	IdentitySourceVersion string `json:"identity_source_version"`
}

type Validity struct {
	NotBefore  string  `json:"not_before"`
	ValidUntil *string `json:"valid_until"`
}

type Quantity struct {
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
	Scale int64  `json:"scale"`
}

type Money struct {
	MinorUnits        int64  `json:"minor_units"`
	Currency          string `json:"currency"`
	MinorUnitExponent int64  `json:"minor_unit_exponent"`
}

type Rational struct {
	Numerator   int64 `json:"numerator"`
	Denominator int64 `json:"denominator"`
}

type Budget struct {
	RoleToolCalls    int64  `json:"role_tool_calls"`
	TaskToolCalls    int64  `json:"task_tool_calls"`
	RoleOutputTokens int64  `json:"role_output_tokens"`
	TaskOutputTokens int64  `json:"task_output_tokens"`
	TaskInputTokens  int64  `json:"task_input_tokens"`
	CostLimit        *Money `json:"cost_limit"`
}

type Windows struct {
	SubmissionMs         int64    `json:"submission_ms"`
	ObjectionMs          int64    `json:"objection_ms"`
	RevisionMs           int64    `json:"revision_ms"`
	FreezeMs             int64    `json:"freeze_ms"`
	ActiveDecisionMs     int64    `json:"active_decision_ms"`
	ArbitrationAcceptMs  int64    `json:"arbitration_accept_ms"`
	ArbitrationFinalMs   int64    `json:"arbitration_final_ms"`
	TaskValidityMs       int64    `json:"task_validity_ms"`
	ReadTimeoutMs        int64    `json:"read_timeout_ms"`
	ReadRetries          int64    `json:"read_retries"`
	FormatRepairsPerRole int64    `json:"format_repairs_per_role"`
	PausedStates         []string `json:"paused_states"`
}

type EvidenceRule struct {
	RuleID                      string        `json:"rule_id"`
	AssertionKind               string        `json:"assertion_kind"`
	SourceKinds                 []string      `json:"source_kinds"`
	MethodRefs                  []ExternalRef `json:"method_refs"`
	QualificationPolicyRef      ExternalRef   `json:"qualification_policy_ref"`
	TTLMs                       int64         `json:"ttl_ms"`
	InvalidatingEvents          []string      `json:"invalidating_events"`
	IndependentVerifierRequired bool          `json:"independent_verifier_required"`
}

type RiskPredicate struct {
	AttributeID string          `json:"attribute_id"`
	Operator    string          `json:"operator"`
	Expected    json.RawMessage `json:"expected,omitempty"`
}

type RiskRule struct {
	RuleID          string          `json:"rule_id"`
	Level           string          `json:"level"`
	AllOf           []RiskPredicate `json:"all_of"`
	EvidenceRuleIDs []string        `json:"evidence_rule_ids"`
}

type RiskPolicy struct {
	RulesVersion       string     `json:"rules_version"`
	RequiredAttributes []string   `json:"required_attributes"`
	Rules              []RiskRule `json:"rules"`
	Aggregation        string     `json:"aggregation"`
	UnknownLevel       string     `json:"unknown_level"`
	Downgrade          string     `json:"downgrade"`
	R3Execution        string     `json:"r3_execution"`
}

type Tool struct {
	ToolID        string      `json:"tool_id"`
	Version       string      `json:"version"`
	DefinitionRef ExternalRef `json:"definition_ref"`
	Mode          string      `json:"mode"`
	ResourceIDs   []string    `json:"resource_ids"`
	RiskLevels    []string    `json:"risk_levels"`
	AdapterMode   string      `json:"adapter_mode"`
}

type Selection struct {
	CandidateLimit                int64            `json:"candidate_limit"`
	DefaultEarlySelectionPolicy   string           `json:"default_early_selection_policy"`
	AllowedEarlySelectionPolicies []string         `json:"allowed_early_selection_policies"`
	MaterialPendingRule           string           `json:"material_pending_rule"`
	ApprovalVotes                 map[string]int64 `json:"approval_votes"`
	TrialRiskLevels               []string         `json:"trial_risk_levels"`
}

type SLATarget struct {
	MetricID             string      `json:"metric_id"`
	PopulationRef        ExternalRef `json:"population_ref"`
	Operator             string      `json:"operator"`
	Bound                Quantity    `json:"bound"`
	MeasurementMethodRef ExternalRef `json:"measurement_method_ref"`
}

type Statistics struct {
	PlanRef               ExternalRef `json:"plan_ref"`
	ClaimCount            int64       `json:"claim_count"`
	FamilyAlpha           Rational    `json:"family_alpha"`
	LabelPolicyRef        ExternalRef `json:"label_policy_ref"`
	DenominatorPolicyRef  ExternalRef `json:"denominator_policy_ref"`
	IndependencePolicyRef ExternalRef `json:"independence_policy_ref"`
	ModelCheckScopeRef    ExternalRef `json:"model_check_scope_ref"`
}

type Retention struct {
	FullInputMs         int64 `json:"full_input_ms"`
	IntegrityMetadataMs int64 `json:"integrity_metadata_ms"`
	CacheMs             int64 `json:"cache_ms"`
	BackupErasureMs     int64 `json:"backup_erasure_ms"`
}

type DomainProfile struct {
	ArtifactType        string              `json:"artifact_type"`
	SchemaVersion       string              `json:"schema_version"`
	Version             string              `json:"version"`
	CreatedBy           SubjectRef          `json:"created_by"`
	IssuancePolicyRef   ExternalRef         `json:"issuance_policy_ref"`
	FrozenAt            string              `json:"frozen_at"`
	Validity            Validity            `json:"validity"`
	DomainKey           string              `json:"domain_key"`
	GovernancePolicyRef ExternalRef         `json:"governance_policy_ref"`
	RiskPolicy          RiskPolicy          `json:"risk_policy"`
	EvidenceRules       []EvidenceRule      `json:"evidence_rules"`
	ToolScope           []Tool              `json:"tool_scope"`
	Selection           Selection           `json:"selection"`
	Budget              Budget              `json:"budget"`
	Windows             Windows             `json:"windows"`
	SLATargets          []SLATarget         `json:"sla_targets"`
	Statistics          Statistics          `json:"statistics"`
	ExperiencePolicyRef ExternalRef         `json:"experience_policy_ref"`
	Retention           Retention           `json:"retention"`
	AdapterPolicyRef    ExternalRef         `json:"adapter_policy_ref"`
	InstanceParameters  []InstanceParameter `json:"instance_parameters"`
}

type InstanceParameter struct {
	ParameterID string   `json:"parameter_id"`
	Value       Quantity `json:"value"`
}
