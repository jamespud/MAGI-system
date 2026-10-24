package inputartifact

// SnapshotRef binds external raw bytes, not a new C0 artifact type. Source and
// qualification policy are independent references, neither grants permission.
type SnapshotRef struct {
	SnapshotID             string      `json:"snapshot_id"`
	Kind                   string      `json:"kind"`
	Version                string      `json:"version"`
	ContentDigest          string      `json:"content_digest"`
	SourceRef              ExternalRef `json:"source_ref"`
	QualificationPolicyRef ExternalRef `json:"qualification_policy_ref"`
	Validity               Validity    `json:"validity"`
}

type RoleView struct {
	PersonaSpecID        string   `json:"persona_spec_id"`
	ExperienceSnapshotID string   `json:"experience_snapshot_id"`
	AllowedContextIDs    []string `json:"allowed_context_ids"`
}

// ContextManifest is the unchanged C1-A / Decision v2 mirror's v1 body.
// Allowed views are access ceilings; this type does not record actual reads.
type ContextManifest struct {
	ArtifactType                 string              `json:"artifact_type"`
	SchemaVersion                string              `json:"schema_version"`
	Version                      string              `json:"version"`
	CreatedBy                    SubjectRef          `json:"created_by"`
	IssuancePolicyRef            ExternalRef         `json:"issuance_policy_ref"`
	FrozenAt                     string              `json:"frozen_at"`
	Validity                     Validity            `json:"validity"`
	CaseID                       string              `json:"case_id"`
	Round                        int64               `json:"round"`
	CharterID                    string              `json:"charter_id"`
	DomainProfileID              string              `json:"domain_profile_id"`
	SharedFactSnapshotID         string              `json:"shared_fact_snapshot_id"`
	UserPreferenceSnapshotID     string              `json:"user_preference_snapshot_id"`
	Roles                        map[string]RoleView `json:"roles"`
	SnapshotRefs                 []SnapshotRef       `json:"snapshot_refs"`
	SourceDependencyIndexVersion string              `json:"source_dependency_index_version"`
	ContextRevocationEpoch       int64               `json:"context_revocation_epoch"`
	ContextRevocationScopeID     string              `json:"context_revocation_scope_id"`
	AdmissionPolicyRef           ExternalRef         `json:"admission_policy_ref"`
}

// ContextCharterBinding is an externally supplied selection declaration, NOT a
// validated Charter or signed approval. C1-C1 compares declared identity/domain/
// version/interval only. Charter bytes, confirmation, disclosure constraints,
// authority and consumer compatibility always remain UNVERIFIABLE_REFERENCE.
// This slice accepts explicit schema_version=2.0 declarations only.
type ContextCharterBinding struct {
	ArtifactID      string
	SchemaVersion   string
	Version         string
	DomainProfileID string
	Validity        Validity
	ValidityHorizon string
}

// ContextSelection must originate outside the body being checked. In production
// its source must be independently authenticated by C2. It is never an authority
// token. In particular a caller cannot prove role ownership by copying a body.
type ContextSelection struct {
	CaseID                       string
	Round                        int64
	Charter                      ContextCharterBinding
	DomainProfileVersion         string
	SharedFactSnapshotID         string
	UserPreferenceSnapshotID     string
	Roles                        map[string]RoleView
	SourceDependencyIndexVersion string
	ContextRevocationScopeID     string
	ContextRevocationEpoch       int64
	AdmissionPolicyRef           ExternalRef
}

// ContextSnapshot supplies a separate expected descriptor and optional original
// raw bytes. nil bytes mean unavailable; a nonnil empty slice is hashed as empty.
// Digest equality never implies source authenticity, consent or qualification.
type ContextSnapshot struct {
	Descriptor SnapshotRef
	RawBytes   []byte
}

type ContextValidationOptions struct {
	// Nonpositive body entries use C1-A's provisional engineering limits:
	// 262144 bytes, depth 24, 4096 members, 16384 string bytes. Positive entries
	// override a ceiling, matching existing offline consumer options.
	Limits                       ResourceLimits
	EnvelopeLimits               ResourceLimits
	RequireCompleteQualification bool
	// RequireCompleteBindings requires every local selection, validated domain/
	// persona, descriptor and raw snapshot, with all Persona DomainRelations
	// CHECKED_OFFLINE. Partial relations fail closed; this cannot grant qualification.
	RequireCompleteBindings bool
	Selection               *ContextSelection
	DomainProfile           *ValidatedDomainProfile
	Personas                map[string]ValidatedPersonaSpec
	Snapshots               map[string]ContextSnapshot
	// UsageWindow is explicit and deterministic; no local wall clock is consulted.
	// It cannot start before frozen_at; its entire interval must fit every known
	// dependency and Charter horizon. Equal timestamps do not prove runtime order.
	UsageWindow *Validity
	// Original snapshots have independent per-blob and aggregate byte ceilings.
	// Nonpositive values default to 262144 / 1048576; bytes are never rewritten.
	MaxSnapshotBytes      int
	MaxTotalSnapshotBytes int
}

type ContextValidationStatus struct {
	Structure     string `json:"structure"`
	Bindings      string `json:"bindings"`
	SnapshotBytes string `json:"snapshot_bytes"`
	Qualification string `json:"qualification"`
	ActualReads   string `json:"actual_reads"`
}
