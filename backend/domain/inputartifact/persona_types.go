package inputartifact

// PersonaSpec mirrors the C1-A Schema. It declares review obligations, not
// completed investigations, publication approval or runtime permissions.
type PersonaSpec struct {
	ArtifactType              string               `json:"artifact_type"`
	SchemaVersion             string               `json:"schema_version"`
	Version                   string               `json:"version"`
	CreatedBy                 SubjectRef           `json:"created_by"`
	IssuancePolicyRef         ExternalRef          `json:"issuance_policy_ref"`
	FrozenAt                  string               `json:"frozen_at"`
	Validity                  Validity             `json:"validity"`
	PersonaID                 string               `json:"persona_id"`
	SpecVersion               string               `json:"spec_version"`
	BaselineID                string               `json:"baseline_id"`
	MaintenancePolicyRef      ExternalRef          `json:"maintenance_policy_ref"`
	CorePriority              []string             `json:"core_priority"`
	MandatoryReviews          []string             `json:"mandatory_reviews"`
	MinimumInvestigation      []Investigation      `json:"minimum_investigation"`
	EvidencePriority          []string             `json:"evidence_priority"`
	RequiredOutputFields      []string             `json:"required_output_fields"`
	InvestigationOutputFields []string             `json:"investigation_output_fields"`
	ToolCallLimit             int64                `json:"tool_call_limit"`
	OutputTokenLimit          int64                `json:"output_token_limit"`
	CallAllocation            []Allocation         `json:"call_allocation"`
	TransferPolicy            string               `json:"transfer_policy"`
	SupportConditions         []string             `json:"support_conditions"`
	RejectConditions          []string             `json:"reject_conditions"`
	AbstainConditions         []string             `json:"abstain_conditions"`
	CorrectionTriggers        []string             `json:"correction_triggers"`
	DomainExtensions          []PersonaExtension   `json:"domain_extensions"`
	Compatibility             PersonaCompatibility `json:"compatibility"`
}

type Investigation struct {
	RequirementID             string   `json:"requirement_id"`
	Description               string   `json:"description"`
	AcceptableEvidenceRuleIDs []string `json:"acceptable_evidence_rule_ids"`
	Substitution              string   `json:"substitution"`
}
type Allocation struct {
	BucketID string `json:"bucket_id"`
	Calls    int64  `json:"calls"`
}
type PersonaExtension struct {
	ExtensionID          string          `json:"extension_id"`
	DomainProfileID      string          `json:"domain_profile_id"`
	ReviewItemIDs        []string        `json:"review_item_ids"`
	MinimumInvestigation []Investigation `json:"minimum_investigation"`
	ApprovalPolicyRef    ExternalRef     `json:"approval_policy_ref"`
}
type PersonaCompatibility struct {
	CoreChange            string      `json:"core_change"`
	ContinuityPolicyRef   ExternalRef `json:"continuity_policy_ref"`
	BehaviorAcceptanceRef ExternalRef `json:"behavior_acceptance_ref"`
}

// PersonaValidationOptions supplies local evidence and ceilings, never authority.
// A selected domain requires both its exact ID and a nonzero validated value.
// No baseline override or resolver capable of granting qualification exists.
type PersonaValidationOptions struct {
	Limits                       ResourceLimits
	EnvelopeLimits               ResourceLimits
	RequireCompleteQualification bool
	RequireDomain                bool
	DomainProfileID              string
	DomainProfile                *ValidatedDomainProfile
	// AdditionalBudgets must include every applicable Charter/engineering cap.
	// No Charter parsing or execution authorization is implemented here.
	AdditionalBudgets []Budget
}

// PersonaValidationStatus separates four different evidence boundaries.
// CHECKED_OFFLINE means declared cross-artifact relations only, not applicable
// real evidence, release registration, current validity or behavior acceptance.
type PersonaValidationStatus struct {
	Baseline        string `json:"baseline"`
	DomainRelations string `json:"domain_relations"`
	Qualification   string `json:"qualification"`
	Behavior        string `json:"behavior"`
}
