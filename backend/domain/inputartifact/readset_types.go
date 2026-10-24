package inputartifact

// Frozen v2 wire types. Declarations carry no authority or proof of real reads.
type JournalKey struct {
	CaseID            string `json:"case_id"`
	Round             int64  `json:"round"`
	ContextManifestID string `json:"context_manifest_id"`
	PersonaID         string `json:"persona_id"`
}

type SourceNode struct {
	NodeID              string      `json:"node_id"`
	SourceEntryID       string      `json:"source_entry_id"`
	SourceVersion       string      `json:"source_version"`
	RawContentDigest    string      `json:"raw_content_digest"`
	Kind                string      `json:"kind"`
	SourceRef           ExternalRef `json:"source_ref"`
	Validity            Validity    `json:"validity"`
	DerivationMethodRef ExternalRef `json:"derivation_method_ref"`
	ParentNodeIDs       []string    `json:"parent_node_ids"`
}

type ReadEvent struct {
	JournalKey            JournalKey  `json:"journal_key"`
	Sequence              int64       `json:"sequence"`
	AuthoritySequence     int64       `json:"authority_sequence"`
	ExecutionGeneration   int64       `json:"execution_generation"`
	DeliveryID            string      `json:"delivery_id"`
	Kind                  string      `json:"kind"`
	Phase                 string      `json:"phase"`
	Source                SourceNode  `json:"source"`
	ParentNodeIDs         []string    `json:"parent_node_ids"`
	PermissionRef         ExternalRef `json:"permission_ref"`
	Producer              SubjectRef  `json:"producer"`
	VerificationRecordRef ExternalRef `json:"verification_record_ref"`
	InjectedAt            string      `json:"injected_at"`
	TargetPersona         string      `json:"target_persona"`
}

type ReadSetSnapshot struct {
	ArtifactType                  string       `json:"artifact_type"`
	SchemaVersion                 string       `json:"schema_version"`
	Version                       string       `json:"version"`
	CreatedBy                     SubjectRef   `json:"created_by"`
	IssuancePolicyRef             ExternalRef  `json:"issuance_policy_ref"`
	FrozenAt                      string       `json:"frozen_at"`
	Validity                      Validity     `json:"validity"`
	JournalKey                    JournalKey   `json:"journal_key"`
	CandidateSnapshotID           string       `json:"candidate_snapshot_id"`
	PersonaSpecID                 string       `json:"persona_spec_id"`
	ExperienceSnapshotID          string       `json:"experience_snapshot_id"`
	UserPreferenceSnapshotID      string       `json:"user_preference_snapshot_id"`
	ExecutionGeneration           int64        `json:"execution_generation"`
	ContextRevocationScopeID      string       `json:"context_revocation_scope_id"`
	ContextRevocationEpoch        int64        `json:"context_revocation_epoch"`
	AuthorityScopeID              string       `json:"authority_scope_id"`
	AuthorityEpoch                int64        `json:"authority_epoch"`
	CredentialScopeID             string       `json:"credential_scope_id"`
	CredentialEpoch               int64        `json:"credential_epoch"`
	SourceDependencyIndexVersion  string       `json:"source_dependency_index_version"`
	Events                        []ReadEvent  `json:"events"`
	DependencyNodes               []SourceNode `json:"dependency_nodes"`
	JournalHighWatermark          int64        `json:"journal_high_watermark"`
	EvidenceCutoffSequence        int64        `json:"evidence_cutoff_sequence"`
	FinalSharedInputs             SnapshotRef  `json:"final_shared_inputs"`
	FinalSharedDeliveryReceiptRef ExternalRef  `json:"final_shared_delivery_receipt_ref"`
	EvidenceClosureRecordRef      ExternalRef  `json:"evidence_closure_record_ref"`
	CompletenessRecordRef         ExternalRef  `json:"completeness_record_ref"`
}

type TrustedDeliveryReceipt struct {
	RecordType                  string      `json:"record_type"`
	RecordVersion               string      `json:"record_version"`
	JournalKey                  JournalKey  `json:"journal_key"`
	PersonaSpecID               string      `json:"persona_spec_id"`
	ExperienceSnapshotID        string      `json:"experience_snapshot_id"`
	UserPreferenceSnapshotID    string      `json:"user_preference_snapshot_id"`
	ControlledSessionID         string      `json:"controlled_session_id"`
	ProviderRequestID           string      `json:"provider_request_id"`
	FinalSharedInputs           SnapshotRef `json:"final_shared_inputs"`
	RegisteredInjectionSequence int64       `json:"registered_injection_sequence"`
	ExecutionGeneration         int64       `json:"execution_generation"`
	ContextRevocationScopeID    string      `json:"context_revocation_scope_id"`
	ContextRevocationEpoch      int64       `json:"context_revocation_epoch"`
	AuthorityScopeID            string      `json:"authority_scope_id"`
	AuthorityEpoch              int64       `json:"authority_epoch"`
	CredentialScopeID           string      `json:"credential_scope_id"`
	CredentialEpoch             int64       `json:"credential_epoch"`
	QuiescenceFenceID           string      `json:"quiescence_fence_id"`
	DeliveryStatus              string      `json:"delivery_status"`
	VerificationRecordRef       ExternalRef `json:"verification_record_ref"`
	VerifiedBy                  SubjectRef  `json:"verified_by"`
	VerifiedAt                  string      `json:"verified_at"`
}

type EvidenceClosureRecord struct {
	RecordType               string                 `json:"record_type"`
	RecordVersion            string                 `json:"record_version"`
	CaseID                   string                 `json:"case_id"`
	Round                    int64                  `json:"round"`
	ContextManifestID        string                 `json:"context_manifest_id"`
	CandidateSnapshotID      string                 `json:"candidate_snapshot_id"`
	PersonaViews             map[string]RoleView    `json:"persona_views"`
	FinalSharedInputs        SnapshotRef            `json:"final_shared_inputs"`
	ReceiptRefs              map[string]ExternalRef `json:"receipt_refs"`
	ConfirmationRefs         map[string]ExternalRef `json:"confirmation_refs"`
	ExecutionGeneration      int64                  `json:"execution_generation"`
	ContextRevocationScopeID string                 `json:"context_revocation_scope_id"`
	ContextRevocationEpoch   int64                  `json:"context_revocation_epoch"`
	AuthorityScopeID         string                 `json:"authority_scope_id"`
	AuthorityEpoch           int64                  `json:"authority_epoch"`
	CredentialScopeID        string                 `json:"credential_scope_id"`
	CredentialEpoch          int64                  `json:"credential_epoch"`
	QuiescenceFenceID        string                 `json:"quiescence_fence_id"`
	EvidenceCutoffSequence   int64                  `json:"evidence_cutoff_sequence"`
	CloseHighWatermarks      map[string]int64       `json:"close_high_watermarks"`
	PendingInjections        int64                  `json:"pending_injections"`
	JournalStates            map[string]string      `json:"journal_states"`
	Event                    string                 `json:"event"`
	Status                   string                 `json:"status"`
	CommitID                 string                 `json:"commit_id"`
	CommittedAt              string                 `json:"committed_at"`
}

type CompletenessRecord struct {
	RecordType           string      `json:"record_type"`
	RecordVersion        string      `json:"record_version"`
	JournalKey           JournalKey  `json:"journal_key"`
	Events               []ReadEvent `json:"events"`
	JournalHighWatermark int64       `json:"journal_high_watermark"`
	DeliverySettled      bool        `json:"delivery_settled"`
	RecordedBy           SubjectRef  `json:"recorded_by"`
	RecordedAt           string      `json:"recorded_at"`
}

// ReadSetSelection is the independently supplied exact journal and scoped epochs.
// ExecutionGeneration is worker ownership, never a context/credential epoch.
type ReadSetSelection struct {
	JournalKey               JournalKey
	ExecutionGeneration      int64
	ContextRevocationScopeID string
	ContextRevocationEpoch   int64
	AuthorityScopeID         string
	AuthorityEpoch           int64
	CredentialScopeID        string
	CredentialEpoch          int64
}

// ReadSetSourceBinding is an immutable external dependency-index declaration.
// ContextSnapshotID declares membership in a Manifest allowed snapshot (or the
// exact selected Candidate). It does not authenticate that membership or origin.
// Every ancestor needs its own binding; derived labels grant no additional scope.
type ReadSetSourceBinding struct {
	Node               SourceNode
	ContextSnapshotID  string
	AdmittedAtSequence int64
}

// Original external bytes use raw SHA-256, without canonical rewriting.
type ReadSetMaterial struct {
	Reference ExternalRef
	RawBytes  []byte
}
type ReadSetCandidate struct {
	ArtifactID string
	RawBytes   []byte
}
type ReadSetValidationOptions struct {
	// Version of the independently supplied immutable dependency-index declaration.
	SourceDependencyIndexVersion string
	Limits                       ResourceLimits
	EnvelopeLimits               ResourceLimits
	RequireCompleteQualification bool
	RequireActualReadProof       bool
	RequireCompleteBindings      bool
	ContextManifest              *ValidatedContextManifest
	Selection                    *ReadSetSelection
	Candidate                    *ReadSetCandidate
	Sources                      map[string]ReadSetSourceBinding
	// Delivery registry entries must match the complete event, including key,
	// sequence and original declaration. Retrying validation is idempotent.
	Deliveries map[string]ReadEvent
	// Keys are MaterialKey(ref), preserving namespace/object/version/digest.
	Materials             map[string]ReadSetMaterial
	MaxEvents             int
	MaxNodes              int
	MaxGraphDepth         int
	MaxMaterialBytes      int
	MaxTotalMaterialBytes int
}

func MaterialKey(ref ExternalRef) string { return refKey(ref) }

type ReadSetValidationStatus struct {
	Structure       string `json:"structure"`
	Bindings        string `json:"bindings"`
	ContextBindings string `json:"context_bindings"`
	Completeness    string `json:"completeness"`
	Qualification   string `json:"qualification"`
	ActualReads     string `json:"actual_reads"`
	Authority       string `json:"authority"`
}
