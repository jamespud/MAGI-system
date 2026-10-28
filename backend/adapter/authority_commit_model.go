package magi

import "time"

// Durable v2 rows mirror S33. The migration owns keys and CHECK constraints;
// these models are not runtime/bootstrap wiring or exposure authorization.
type authorityHeadRow struct {
	TrustDomain            string    `gorm:"column:trust_domain;primaryKey"`
	LastSequence           uint64    `gorm:"column:last_sequence"`
	LastAdmissionSequence  uint64    `gorm:"column:last_admission_sequence"`
	LastRevocationSequence uint64    `gorm:"column:last_revocation_sequence"`
	LastGateSequence       uint64    `gorm:"column:last_gate_sequence"`
	LastSettlementSequence uint64    `gorm:"column:last_settlement_sequence"`
	UpdatedAt              time.Time `gorm:"column:updated_at"`
}

func (authorityHeadRow) TableName() string { return "magi_authority_head" }

type authorityJournalV2Row struct {
	TrustDomain         string    `gorm:"column:trust_domain;primaryKey"`
	JournalID           string    `gorm:"column:journal_id;primaryKey"`
	CaseID              string    `gorm:"column:case_id"`
	CaseRound           uint64    `gorm:"column:case_round"`
	ContextManifestID   string    `gorm:"column:context_manifest_id"`
	PersonaID           string    `gorm:"column:persona_id"`
	ExecutionGeneration uint64    `gorm:"column:execution_generation"`
	HighWatermark       uint64    `gorm:"column:high_watermark"`
	PendingCount        uint64    `gorm:"column:pending_count"`
	RowVersion          uint64    `gorm:"column:row_version"`
	State               string    `gorm:"column:state"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (authorityJournalV2Row) TableName() string { return "magi_authority_journal_v2" }

type authorityAdmissionV2Row struct {
	TrustDomain          string    `gorm:"column:trust_domain;primaryKey"`
	CommitID             string    `gorm:"column:commit_id;primaryKey"`
	UniqueKey            string    `gorm:"column:unique_key"`
	CaseID               string    `gorm:"column:case_id"`
	CaseRound            uint64    `gorm:"column:case_round"`
	ContextManifestID    string    `gorm:"column:context_manifest_id"`
	PersonaID            string    `gorm:"column:persona_id"`
	DeliveryID           string    `gorm:"column:delivery_id"`
	ProviderRequestID    string    `gorm:"column:provider_request_id"`
	JournalID            string    `gorm:"column:journal_id"`
	RequestDigest        string    `gorm:"column:request_digest"`
	ExactTargetID        string    `gorm:"column:exact_target_id"`
	CommitToken          string    `gorm:"column:commit_token"`
	AuthoritySequence    uint64    `gorm:"column:authority_sequence"`
	JournalHighWatermark uint64    `gorm:"column:journal_high_watermark"`
	ExecutionGeneration  uint64    `gorm:"column:execution_generation"`
	ProofID              string    `gorm:"column:proof_id"`
	AdmissionDigest      string    `gorm:"column:admission_digest"`
	AdmissionObjectRef   string    `gorm:"column:admission_object_ref"`
	RawRequest           []byte    `gorm:"column:raw_request"`
	OwnerJobID           string    `gorm:"column:owner_job_id"`
	OwnerWorkerID        string    `gorm:"column:owner_worker_id"`
	OwnerLeaseUntil      time.Time `gorm:"column:owner_lease_until"`
	State                string    `gorm:"column:state"`
	ExposurePermitted    bool      `gorm:"column:exposure_permitted"`
	CommittedAt          time.Time `gorm:"column:committed_at"`
}

func (authorityAdmissionV2Row) TableName() string { return "magi_authority_admission_v2" }

type authorityJournalEventV2Row struct {
	TrustDomain          string    `gorm:"column:trust_domain;primaryKey"`
	JournalID            string    `gorm:"column:journal_id;primaryKey"`
	JournalHighWatermark uint64    `gorm:"column:journal_high_watermark;primaryKey"`
	CommitID             string    `gorm:"column:commit_id"`
	EventID              string    `gorm:"column:event_id"`
	EventDigest          string    `gorm:"column:event_digest"`
	ObjectRef            string    `gorm:"column:object_ref"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (authorityJournalEventV2Row) TableName() string { return "magi_authority_journal_event_v2" }

type authorityDependencyV2Row struct {
	TrustDomain      string    `gorm:"column:trust_domain;primaryKey"`
	CommitID         string    `gorm:"column:commit_id;primaryKey"`
	DependencyID     string    `gorm:"column:dependency_id;primaryKey"`
	DependencyDigest string    `gorm:"column:dependency_digest"`
	ObjectRef        string    `gorm:"column:object_ref"`
	Ordinal          uint64    `gorm:"column:ordinal"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (authorityDependencyV2Row) TableName() string { return "magi_authority_dependency_v2" }

type authorityProviderReservationV2Row struct {
	TrustDomain       string    `gorm:"column:trust_domain;primaryKey"`
	CommitID          string    `gorm:"column:commit_id;primaryKey"`
	ProviderRequestID string    `gorm:"column:provider_request_id"`
	SessionScopeID    string    `gorm:"column:session_scope_id"`
	State             string    `gorm:"column:state"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (authorityProviderReservationV2Row) TableName() string {
	return "magi_authority_provider_reservation_v2"
}

type authorityExposureFenceV2Row struct {
	TrustDomain              string    `gorm:"column:trust_domain;primaryKey"`
	CommitID                 string    `gorm:"column:commit_id;primaryKey"`
	FenceID                  string    `gorm:"column:fence_id"`
	Mode                     string    `gorm:"column:mode"`
	State                    string    `gorm:"column:state"`
	ExposurePermitted        bool      `gorm:"column:exposure_permitted"`
	LeaseID                  string    `gorm:"column:lease_id"`
	LeaseUntil               time.Time `gorm:"column:lease_until"`
	AuthorityWatermark       uint64    `gorm:"column:authority_watermark"`
	ExecutionGeneration      uint64    `gorm:"column:execution_generation"`
	ScopedEpochs             []byte    `gorm:"column:scoped_epochs"`
	ProviderCapabilityID     string    `gorm:"column:provider_capability_id"`
	ProviderCapabilityDigest string    `gorm:"column:provider_capability_digest"`
	ProviderCapabilityRef    string    `gorm:"column:provider_capability_ref"`
	CreatedAt                time.Time `gorm:"column:created_at"`
}

func (authorityExposureFenceV2Row) TableName() string { return "magi_authority_exposure_fence_v2" }

type authorityPendingTransitionV2Row struct {
	TrustDomain     string    `gorm:"column:trust_domain;primaryKey"`
	CommitID        string    `gorm:"column:commit_id;primaryKey"`
	JournalID       string    `gorm:"column:journal_id"`
	ExpectedPending uint64    `gorm:"column:expected_pending"`
	NextPending     uint64    `gorm:"column:next_pending"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (authorityPendingTransitionV2Row) TableName() string {
	return "magi_authority_pending_transition_v2"
}

type authorityUniqueBindingV2Row struct {
	TrustDomain          string    `gorm:"column:trust_domain;primaryKey"`
	UniqueKey            string    `gorm:"column:unique_key;primaryKey"`
	CommitID             string    `gorm:"column:commit_id"`
	RequestDigest        string    `gorm:"column:request_digest"`
	ExactTargetID        string    `gorm:"column:exact_target_id"`
	CommitToken          string    `gorm:"column:commit_token"`
	AuthoritySequence    uint64    `gorm:"column:authority_sequence"`
	JournalHighWatermark uint64    `gorm:"column:journal_high_watermark"`
	ExecutionGeneration  uint64    `gorm:"column:execution_generation"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (authorityUniqueBindingV2Row) TableName() string { return "magi_authority_unique_binding_v2" }

type authorityTokenConsumedV2Row struct {
	TrustDomain   string    `gorm:"column:trust_domain;primaryKey"`
	CommitToken   string    `gorm:"column:commit_token;primaryKey"`
	CommitID      string    `gorm:"column:commit_id"`
	RequestDigest string    `gorm:"column:request_digest"`
	ConsumedAt    time.Time `gorm:"column:consumed_at"`
}

func (authorityTokenConsumedV2Row) TableName() string { return "magi_authority_token_consumed_v2" }

type authorityLineageV2Row struct {
	TrustDomain          string    `gorm:"column:trust_domain;primaryKey"`
	CommitID             string    `gorm:"column:commit_id;primaryKey"`
	RequestDigest        string    `gorm:"column:request_digest"`
	LineageDigest        string    `gorm:"column:lineage_digest"`
	AuthoritySequence    uint64    `gorm:"column:authority_sequence"`
	JournalHighWatermark uint64    `gorm:"column:journal_high_watermark"`
	RawLineage           []byte    `gorm:"column:raw_lineage"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (authorityLineageV2Row) TableName() string { return "magi_authority_lineage_v2" }

type authorityOutboxV2Row struct {
	TrustDomain string    `gorm:"column:trust_domain;primaryKey"`
	CommitID    string    `gorm:"column:commit_id;primaryKey"`
	EventKind   string    `gorm:"column:event_kind"`
	Payload     []byte    `gorm:"column:payload"`
	State       string    `gorm:"column:state"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (authorityOutboxV2Row) TableName() string { return "magi_authority_outbox_v2" }
