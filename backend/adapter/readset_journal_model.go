package magi

import "time"

// ReadSetJournalModel is the durable local per-journal header. Its owner fields
// are diagnostic/durable bindings, not authorization credentials. Every append
// is fenced by the existing DecisionJob lease + Case ExecutionGeneration.
type ReadSetJournalModel struct {
	ID                           string    `gorm:"column:id;primaryKey;size:96"`
	CaseID                       string    `gorm:"column:case_id;size:64;not null"`
	RoundNo                      int64     `gorm:"column:round_no;not null"`
	ContextManifestID            string    `gorm:"column:context_manifest_id;size:191;not null"`
	PersonaID                    string    `gorm:"column:persona_id;size:32;not null"`
	State                        string    `gorm:"column:state;size:32;not null"`
	HighWatermark                int64     `gorm:"column:high_watermark;not null"`
	PendingCount                 int64     `gorm:"column:pending_count;not null"`
	EventBytes                   int64     `gorm:"column:event_bytes;not null"`
	SourceDependencyIndexVersion string    `gorm:"column:source_dependency_index_version;size:191;not null"`
	AdmissionScopeID             string    `gorm:"column:admission_scope_id;size:191;not null"`
	ContextRevocationScopeID     string    `gorm:"column:context_revocation_scope_id;size:191;not null"`
	ContextRevocationEpoch       int64     `gorm:"column:context_revocation_epoch;not null"`
	AuthorityScopeID             string    `gorm:"column:authority_scope_id;size:191;not null"`
	AuthorityEpoch               int64     `gorm:"column:authority_epoch;not null"`
	CredentialScopeID            string    `gorm:"column:credential_scope_id;size:191;not null"`
	CredentialEpoch              int64     `gorm:"column:credential_epoch;not null"`
	OwnerJobID                   string    `gorm:"column:owner_job_id;size:64;not null"`
	OwnerWorkerID                string    `gorm:"column:owner_worker_id;size:255;not null"`
	ExecutionGeneration          int64     `gorm:"column:execution_generation;not null"`
	OwnerLeaseUntil              time.Time `gorm:"column:owner_lease_until;not null"`
	StateVersion                 int64     `gorm:"column:state_version;not null"`
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

func (ReadSetJournalModel) TableName() string { return "readset_journal" }

// ReadSetJournalEventModel stores exact canonical pre-exposure registration
// bytes. It never contains an authoritative sequence or verification claim.
type ReadSetJournalEventModel struct {
	JournalID           string    `gorm:"column:journal_id;primaryKey;size:96"`
	LocalSequence       int64     `gorm:"column:local_sequence;primaryKey;not null"`
	EventVersion        string    `gorm:"column:event_version;size:16;not null"`
	DeliveryID          string    `gorm:"column:delivery_id;size:191;not null"`
	ProviderRequestID   string    `gorm:"column:provider_request_id;size:191;not null"`
	RegistrationState   string    `gorm:"column:registration_state;size:32;not null"`
	ExecutionGeneration int64     `gorm:"column:execution_generation;not null"`
	OwnerJobID          string    `gorm:"column:owner_job_id;size:64;not null"`
	OwnerWorkerID       string    `gorm:"column:owner_worker_id;size:255;not null"`
	OwnerLeaseUntil     time.Time `gorm:"column:owner_lease_until;not null"`
	SourceNodeID        string    `gorm:"column:source_node_id;size:191;not null"`
	SourceVersion       string    `gorm:"column:source_version;size:191;not null"`
	TargetPersona       string    `gorm:"column:target_persona;size:32;not null"`
	PayloadJSON         []byte    `gorm:"column:payload_json;type:longblob;not null"`
	PayloadDigest       string    `gorm:"column:payload_digest;size:64;not null"`
	RequestDigest       string    `gorm:"column:request_digest;size:64;not null"`
	PayloadBytes        int       `gorm:"column:payload_bytes;not null"`
	RegisteredAt        time.Time `gorm:"column:registered_at;not null"`
}

func (ReadSetJournalEventModel) TableName() string { return "readset_journal_event" }

// ReadSetJournalDeliveryModel separates provider lifecycle from registration.
// This slice never writes APPLIED_VERIFIED/FAILED.
type ReadSetJournalDeliveryModel struct {
	JournalID                  string     `gorm:"column:journal_id;primaryKey;size:96"`
	DeliveryID                 string     `gorm:"column:delivery_id;primaryKey;size:191"`
	EventSequence              int64      `gorm:"column:event_sequence;not null"`
	ProviderRequestID          string     `gorm:"column:provider_request_id;size:191;not null"`
	State                      string     `gorm:"column:state;size:32;not null"`
	StateVersion               int64      `gorm:"column:state_version;not null"`
	FirstExecutionGeneration   int64      `gorm:"column:first_execution_generation;not null"`
	LastExecutionGeneration    int64      `gorm:"column:last_execution_generation;not null"`
	LastOwnerJobID             string     `gorm:"column:last_owner_job_id;size:64;not null"`
	LastOwnerWorkerID          string     `gorm:"column:last_owner_worker_id;size:255;not null"`
	LastOwnerLeaseUntil        time.Time  `gorm:"column:last_owner_lease_until;not null"`
	SettledExecutionGeneration *int64     `gorm:"column:settled_execution_generation"`
	PayloadDigest              string     `gorm:"column:payload_digest;size:64;not null"`
	AuthoritySequence          *int64     `gorm:"column:authority_sequence"`
	VerificationRecordJSON     []byte     `gorm:"column:verification_record_json;type:longblob"`
	InjectedAt                 *time.Time `gorm:"column:injected_at"`
	VerifiedAt                 *time.Time `gorm:"column:verified_at"`
	LastReason                 string     `gorm:"column:last_reason;type:text;not null"`
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

func (ReadSetJournalDeliveryModel) TableName() string { return "readset_journal_delivery" }

// ReadSetJournalNodeModel stores one immutable dependency descriptor. Its
// admission_authority_sequence remains NULL until trusted independent evidence
// exists; local sequence is never substituted for that fact.
type ReadSetJournalNodeModel struct {
	JournalID                  string `gorm:"column:journal_id;primaryKey;size:96"`
	NodeID                     string `gorm:"column:node_id;primaryKey;size:191"`
	SourceEntryID              string `gorm:"column:source_entry_id;size:191;not null"`
	SourceVersion              string `gorm:"column:source_version;size:191;not null"`
	Kind                       string `gorm:"column:kind;size:32;not null"`
	NodeDigest                 string `gorm:"column:node_digest;size:64;not null"`
	NodeJSON                   []byte `gorm:"column:node_json;type:longblob;not null"`
	FirstSequence              int64  `gorm:"column:first_sequence;not null"`
	AdmissionGeneration        int64  `gorm:"column:admission_generation;not null"`
	AdmissionJobID             string `gorm:"column:admission_job_id;size:64;not null"`
	AdmissionWorkerID          string `gorm:"column:admission_worker_id;size:255;not null"`
	AdmissionScopeID           string `gorm:"column:admission_scope_id;size:191;not null"`
	AdmissionAuthoritySequence *int64 `gorm:"column:admission_authority_sequence"`
	CreatedAt                  time.Time
}

func (ReadSetJournalNodeModel) TableName() string { return "readset_journal_node" }

type ReadSetJournalEdgeModel struct {
	JournalID       string `gorm:"column:journal_id;primaryKey;size:96"`
	ChildNodeID     string `gorm:"column:child_node_id;primaryKey;size:191"`
	ParentNodeID    string `gorm:"column:parent_node_id;primaryKey;size:191"`
	CreatedSequence int64  `gorm:"column:created_sequence;not null"`
}

func (ReadSetJournalEdgeModel) TableName() string { return "readset_journal_edge" }
