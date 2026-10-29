// Package authoritycommit defines the pre-exposure JOURNAL_ADMIT_V2 contract.
// Validation and identity calculation do not authenticate C2 proofs, commit
// mutations, or grant exposure permission. There is no enabled runtime here.
package authoritycommit

import "github.com/jamespud/magi/backend/domain/authority"

const (
	ContractVersion  = "magi-authority-journal-v2/1"
	JournalAdmitV2   = "JOURNAL_ADMIT_V2"
	JournalAuditRead = "JOURNAL_AUDIT_READ"
)

const JournalExposureGateV2 = "JOURNAL_EXPOSURE_GATE_V2"

// ExposureGateRequest binds one durable attempt to the exact admission,
// provider fence and independently verifiable capability evidence.
type ExposureGateRequest struct {
	ContractVersion string                        `json:"contract_version"`
	Operation       string                        `json:"operation"`
	ExactTargetID   string                        `json:"exact_target_id"`
	CommitToken     string                        `json:"commit_token"`
	AttemptID       string                        `json:"attempt_id"`
	Verification    authority.VerificationRequest `json:"verification"`
	Witness         WitnessEvidence               `json:"witness"`
	Gate            ExposureGatePlan              `json:"gate"`
}

// WitnessEvidence binds an independently persisted predecessor receipt to the
// exact admission and request. Its presence is not proof of independence.
type WitnessEvidence struct {
	WitnessID         string `json:"witness_id"`
	EvidenceID        string `json:"evidence_id"`
	EvidenceDigest    string `json:"evidence_digest"`
	ObjectRef         string `json:"object_ref"`
	AdmissionCommitID string `json:"admission_commit_id"`
	RequestDigest     string `json:"request_digest"`
	ValidUntil        string `json:"valid_until"`
}

// ExposureGatePlan names one attempt and the admission it may gate. Its
// linearization sequence is allocated only when the attempt commits.
type ExposureGatePlan struct {
	GateID                 string             `json:"gate_id"`
	AdmissionCommitID      string             `json:"admission_commit_id"`
	AdmissionRequestDigest string             `json:"admission_request_digest"`
	FenceID                string             `json:"fence_id"`
	LinearizationSequence  uint64             `json:"linearization_sequence"`
	TrustedNow             string             `json:"trusted_now"`
	ProviderCapability     ProviderCapability `json:"provider_capability"`
}

// ProviderCapability is an immutable evidence declaration, not a boolean
// trusted by this package. A commit adapter must verify its signature and binding.
type ProviderCapability struct {
	ProviderID                  string         `json:"provider_id"`
	CapabilityID                string         `json:"capability_id"`
	CapabilityDigest            string         `json:"capability_digest"`
	VerificationRef             PreparedObject `json:"verification_ref"`
	EnforcesFence               bool           `json:"enforces_fence"`
	SupportsLease               bool           `json:"supports_lease"`
	EnforcesRevocationWatermark bool           `json:"enforces_revocation_watermark"`
	MaxLeaseUntil               string         `json:"max_lease_until"`
}

// ExposureGateAttempt is the durable terminal result for one attempt. A
// BLOCKED attempt never receives an authority sequence or a permit.
type ExposureGateAttempt struct {
	AttemptID         string `json:"attempt_id"`
	RequestDigest     string `json:"request_digest"`
	Outcome           string `json:"outcome"`
	Code              string `json:"code"`
	GateID            string `json:"gate_id"`
	AdmissionCommitID string `json:"admission_commit_id"`
	AuthoritySequence uint64 `json:"authority_sequence"`
	CommittedAt       string `json:"committed_at"`
	BlockedReason     string `json:"blocked_reason,omitempty"`
	ExposurePermitted bool   `json:"exposure_permitted"`
}

// ExposureGateResult is never a remote APPLY acknowledgement.
type ExposureGateResult struct {
	Outcome             authority.Outcome    `json:"outcome"`
	Code                string               `json:"code"`
	Detail              string               `json:"detail,omitempty"`
	Disposition         string               `json:"disposition,omitempty"`
	Attempt             *ExposureGateAttempt `json:"attempt"`
	BlockedCapabilities []string             `json:"blocked_capabilities,omitempty"`
	ExposurePermitted   bool                 `json:"exposure_permitted"`
}

// AdmissionRequest binds the frozen admission plan to the exact C2 request and
// commit token. ExactTargetID names Admission.AdmissionInput.ID. Proof and token
// authenticity/currentness must still be verified by the transactional adapter.
type AdmissionRequest struct {
	ContractVersion string                        `json:"contract_version"`
	Operation       string                        `json:"operation"`
	ExactTargetID   string                        `json:"exact_target_id"`
	CommitToken     string                        `json:"commit_token"`
	Verification    authority.VerificationRequest `json:"verification"`
	Admission       AdmissionPlan                 `json:"admission"`
}

// ReplayRequest is a historical audit read, not another admission commit. The
// Original request is lookup material only; only AuditVerification grants the
// right to read its committed lineage. No execution owner or lease is accepted.
type ReplayRequest struct {
	Original          AdmissionRequest              `json:"original"`
	AuditVerification authority.VerificationRequest `json:"audit_verification"`
}

// ExposureGateReplayRequest is the historical Gate read boundary. The exact
// original Gate request is lookup material only; the audit proof authorizes
// returning its durable lineage, never another Gate or a permit.
type ExposureGateReplayRequest struct {
	Original          ExposureGateRequest           `json:"original"`
	AuditVerification authority.VerificationRequest `json:"audit_verification"`
}

type PreparedObject struct {
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	ObjectRef string `json:"object_ref"`
}

// AdmissionKey preserves the frozen wire fields. ProviderRequestID is a request
// binding, not part of the durable unique key; use ProjectAdmissionIdentity.
type AdmissionKey struct {
	CaseID            string `json:"case_id"`
	Round             uint64 `json:"round"`
	ContextManifestID string `json:"context_manifest_id"`
	PersonaID         string `json:"persona_id"`
	DeliveryID        string `json:"delivery_id"`
	ProviderRequestID string `json:"provider_request_id"`
}

// AdmissionIdentity is an immutable snapshot of the six durable key fields.
// Its fields are private scalar values: callers cannot edit an identity after
// projection, and later request changes cannot reset the snapshot's key.
type AdmissionIdentity struct {
	trustDomain       string
	caseID            string
	round             uint64
	contextManifestID string
	personaID         string
	deliveryID        string
}

type HighWatermarkPlan struct {
	JournalID string `json:"journal_id"`
	Expected  uint64 `json:"expected"`
	Next      uint64 `json:"next"`
}

type ProviderReservationPlan struct {
	RequestID      string `json:"request_id"`
	SessionScopeID string `json:"session_scope_id"`
	ExpectedStatus string `json:"expected_status"`
	NextStatus     string `json:"next_status"`
}

type PendingTransitionPlan struct {
	JournalID string `json:"journal_id"`
	Expected  uint64 `json:"expected"`
	Next      uint64 `json:"next"`
}

// ExposureFencePlan is a declaration only. It cannot be consumed as a permit.
type ExposureFencePlan struct {
	Mode                  string                  `json:"mode"`
	LeaseID               string                  `json:"lease_id"`
	LeaseUntil            string                  `json:"lease_until"`
	AuthorityWatermark    uint64                  `json:"authority_watermark"`
	ExecutionGeneration   uint64                  `json:"execution_generation"`
	ScopedEpochs          []authority.ScopedEpoch `json:"scoped_epochs"`
	ProviderCapabilityRef PreparedObject          `json:"provider_capability_ref"`
}

// AdmissionPlan preserves the admission fields of scripts/journal-admit-v2-api.go.
type AdmissionPlan struct {
	Key                  AdmissionKey            `json:"key"`
	Principal            authority.Subject       `json:"principal"`
	ExecutionGeneration  uint64                  `json:"execution_generation"`
	ScopedEpochs         []authority.ScopedEpoch `json:"scoped_epochs"`
	MutationSet          []string                `json:"mutation_set"`
	AdmissionInput       PreparedObject          `json:"admission_input"`
	JournalEventInput    PreparedObject          `json:"journal_event_input"`
	DependencyEdgeInputs []PreparedObject        `json:"dependency_edge_inputs"`
	HighWatermark        HighWatermarkPlan       `json:"high_watermark"`
	ProviderReservation  ProviderReservationPlan `json:"provider_reservation"`
	PendingTransition    PendingTransitionPlan   `json:"pending_transition"`
	Fence                ExposureFencePlan       `json:"fence"`
}

// AdmissionCommit identifies an immutable admission record. AuthoritySequence
// orders authority commits; JournalHighWatermark counts local Journal events.
// Neither counter is an exposure permission, and they are never interchangeable.
type AdmissionCommit struct {
	UniqueKey            string `json:"unique_key"`
	RequestDigest        string `json:"request_digest"`
	AdmissionDigest      string `json:"admission_digest"`
	CommitID             string `json:"commit_id"`
	ExactTargetID        string `json:"exact_target_id"`
	CommitToken          string `json:"commit_token"`
	ProofID              string `json:"proof_id"`
	JournalID            string `json:"journal_id"`
	AuthoritySequence    uint64 `json:"authority_sequence"`
	JournalLocalSequence uint64 `json:"journal_local_sequence"`
	JournalHighWatermark uint64 `json:"journal_high_watermark"`
	PendingBefore        uint64 `json:"pending_before"`
	PendingAfter         uint64 `json:"pending_after"`
	CommittedAt          string `json:"committed_at"`
	LineageDigest        string `json:"lineage_digest"`
	ExposurePermitted    bool   `json:"exposure_permitted"`
}

// Result reports structural validation or a future persisted commit. A VALID
// validation result has no Commit and authorizes no mutation. Admission results,
// including replay, must always leave ExposurePermitted false.
type Result struct {
	Outcome             authority.Outcome `json:"outcome"`
	Code                string            `json:"code"`
	Detail              string            `json:"detail,omitempty"`
	Disposition         string            `json:"disposition,omitempty"`
	Commit              *AdmissionCommit  `json:"commit"`
	BlockedCapabilities []string          `json:"blocked_capabilities,omitempty"`
	ExposurePermitted   bool              `json:"exposure_permitted"`
}
