// Declaration-only additive contract for JOURNAL_ADMIT v2. This file is not
// imported by production code and contains no issuer, transaction, provider or
// exposure implementation.
package main

type V2Operation string

const (
	V2JournalAdmit        V2Operation = "JOURNAL_ADMIT_V2"
	V2JournalExposureGate V2Operation = "JOURNAL_EXPOSURE_GATE_V2"
	V2JournalSettle       V2Operation = "JOURNAL_SETTLE_V2"
)

type V2Subject struct {
	SubjectID             string `json:"subject_id"`
	IdentitySourceID      string `json:"identity_source_id"`
	IdentitySourceVersion string `json:"identity_source_version"`
}

type V2ScopedEpoch struct {
	Kind    string `json:"kind"`
	ScopeID string `json:"scope_id"`
	Epoch   uint64 `json:"epoch"`
}

type V2PreparedObject struct {
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	ObjectRef string `json:"object_ref"`
}

type V2AdmissionKey struct {
	CaseID            string `json:"case_id"`
	Round             uint64 `json:"round"`
	ContextManifestID string `json:"context_manifest_id"`
	PersonaID         string `json:"persona_id"`
	DeliveryID        string `json:"delivery_id"`
	ProviderRequestID string `json:"provider_request_id"`
}

type V2HighWatermarkPlan struct {
	JournalID string `json:"journal_id"`
	Expected  uint64 `json:"expected"`
	Next      uint64 `json:"next"`
}

type V2ProviderReservationPlan struct {
	RequestID      string `json:"request_id"`
	SessionScopeID string `json:"session_scope_id"`
	ExpectedStatus string `json:"expected_status"`
	NextStatus     string `json:"next_status"`
}

type V2PendingTransitionPlan struct {
	JournalID string `json:"journal_id"`
	Expected  uint64 `json:"expected"`
	Next      uint64 `json:"next"`
}

type V2ExposureFencePlan struct {
	Mode                  string           `json:"mode"`
	LeaseID               string           `json:"lease_id"`
	LeaseUntil            string           `json:"lease_until"`
	AuthorityWatermark    uint64           `json:"authority_watermark"`
	ExecutionGeneration   uint64           `json:"execution_generation"`
	ScopedEpochs          []V2ScopedEpoch  `json:"scoped_epochs"`
	ProviderCapabilityRef V2PreparedObject `json:"provider_capability_ref"`
}

type V2AdmissionPlan struct {
	Key                  V2AdmissionKey            `json:"key"`
	Principal            V2Subject                 `json:"principal"`
	ExecutionGeneration  uint64                    `json:"execution_generation"`
	ScopedEpochs         []V2ScopedEpoch           `json:"scoped_epochs"`
	MutationSet          []string                  `json:"mutation_set"`
	AdmissionInput       V2PreparedObject          `json:"admission_input"`
	JournalEventInput    V2PreparedObject          `json:"journal_event_input"`
	DependencyEdgeInputs []V2PreparedObject        `json:"dependency_edge_inputs"`
	HighWatermark        V2HighWatermarkPlan       `json:"high_watermark"`
	ProviderReservation  V2ProviderReservationPlan `json:"provider_reservation"`
	PendingTransition    V2PendingTransitionPlan   `json:"pending_transition"`
	Fence                V2ExposureFencePlan       `json:"fence"`
}

type V2ExposureGatePlan struct {
	GateID                string `json:"gate_id"`
	AdmissionCommitID     string `json:"admission_commit_id"`
	FenceID               string `json:"fence_id"`
	LinearizationSequence uint64 `json:"linearization_sequence"`
	TrustedNow            string `json:"trusted_now"`
}

type V2ProviderReceipt struct {
	ReceiptID         string           `json:"receipt_id"`
	Verified          bool             `json:"verified"`
	ProviderRequestID string           `json:"provider_request_id"`
	SessionScopeID    string           `json:"session_scope_id"`
	AppliedAt         string           `json:"applied_at"`
	VerificationRef   V2PreparedObject `json:"verification_ref"`
	Outcome           string           `json:"outcome"`
}

type V2SettlementPlan struct {
	AdmissionCommitID string `json:"admission_commit_id"`
	ProviderRequestID string `json:"provider_request_id"`
	ExpectedStatus    string `json:"expected_status"`
	NextStatus        string `json:"next_status"`
	// Domain-separated digest over the exact settlement body, receipt and verification ref.
	SettlementDigest  string                  `json:"settlement_digest"`
	Receipt           V2ProviderReceipt       `json:"receipt"`
	PendingTransition V2PendingTransitionPlan `json:"pending_transition"`
	MutationSet       []string                `json:"mutation_set"`
}

type V2AuthorityState struct {
	AdmissionCommitID      string `json:"admission_commit_id"`
	AdmissionSequence      uint64 `json:"admission_sequence"`
	RevocationSequence     uint64 `json:"revocation_sequence"`
	RevocationScopeID      string `json:"revocation_scope_id"`
	RevocationEpoch        uint64 `json:"revocation_epoch"`
	GateSequence           uint64 `json:"gate_sequence"`
	SettlementSequence     uint64 `json:"settlement_sequence"`
	CurrentGeneration      uint64 `json:"current_generation"`
	CurrentAuthorityEpoch  uint64 `json:"current_authority_epoch"`
	CurrentContextEpoch    uint64 `json:"current_context_epoch"`
	CurrentCredentialEpoch uint64 `json:"current_credential_epoch"`
	TrustedNow             string `json:"trusted_now"`
}

type V2ProviderCapability struct {
	Available                   bool   `json:"available"`
	EnforcesFence               bool   `json:"enforces_fence"`
	SupportsLease               bool   `json:"supports_lease"`
	EnforcesRevocationWatermark bool   `json:"enforces_revocation_watermark"`
	MaxLeaseUntil               string `json:"max_lease_until"`
}

type V2AdmissionBinding struct {
	RequestDigest     string `json:"request_digest"`
	AdmissionCommitID string `json:"admission_commit_id"`
	Generation        uint64 `json:"generation"`
	Status            string `json:"status"`
	ExposurePermitted bool   `json:"exposure_permitted"`
}

// V2SettlementBinding is created atomically by the first settlement commit.
// It is absent before that commit and is the exact replay key afterwards.
type V2SettlementBinding struct {
	SettlementDigest   string `json:"settlement_digest"`
	AdmissionCommitID  string `json:"admission_commit_id"`
	ProviderRequestID  string `json:"provider_request_id"`
	SettlementCommitID string `json:"settlement_commit_id"`
	Generation         uint64 `json:"generation"`
	Status             string `json:"status"`
}

type V2Trace struct {
	ContractVersion   string               `json:"contract_version"`
	Operation         V2Operation          `json:"operation"`
	Admission         V2AdmissionPlan      `json:"admission"`
	ExposureGate      *V2ExposureGatePlan  `json:"exposure_gate"`
	Settlement        *V2SettlementPlan    `json:"settlement"`
	Authority         V2AuthorityState     `json:"authority"`
	Provider          V2ProviderCapability `json:"provider"`
	AdmissionBinding  *V2AdmissionBinding  `json:"admission_binding"`
	SettlementBinding *V2SettlementBinding `json:"settlement_binding"`
	RequestDigest     string               `json:"request_digest"`
}
