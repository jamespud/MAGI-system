// Package authoritycommit defines the pre-exposure JOURNAL_ADMIT_V2 contract.
// Validation and identity calculation do not authenticate C2 proofs, commit
// mutations, or grant exposure permission. There is no enabled runtime here.
package authoritycommit

import "github.com/jamespud/magi/backend/domain/authority"

const (
	ContractVersion = "magi-authority-journal-v2/1"
	JournalAdmitV2  = "JOURNAL_ADMIT_V2"
)

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
