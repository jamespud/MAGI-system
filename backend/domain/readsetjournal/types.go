// Package readsetjournal defines the local, pre-exposure ReadSet journal
// registration contract and its storage-independent invariants.
//
// A registered event is not proof that a provider applied material or that a
// model read it. This package has no authoritative sequence allocator and no
// APPLIED_VERIFIED transition.
package readsetjournal

import (
	"errors"
	"time"

	"github.com/jamespud/magi/backend/domain/inputartifact"
)

const (
	// EventVersion identifies the local registration payload, not the frozen
	// ReadSet v2 wire version and not the issue #45 authority contract version.
	EventVersion = "1.0"

	// Protocol is the local storage protocol name used in diagnostic output.
	Protocol = "magi-readset-journal-registration/v1"
)

var (
	ErrDisabled          = errors.New("readset journal capability disabled")
	ErrOwnerFence        = errors.New("readset journal owner fence rejected")
	ErrInvalidJournal    = errors.New("invalid readset journal registration")
	ErrDependency        = errors.New("invalid readset journal dependency closure")
	ErrLimit             = errors.New("readset journal resource limit exceeded")
	ErrDeliveryConflict  = errors.New("readset journal delivery identity conflict")
	ErrJournalScope      = errors.New("readset journal admission scope conflict")
	ErrRevoked           = errors.New("readset journal scope is no longer current")
	ErrNotFound          = errors.New("readset journal record not found")
	ErrProviderBlocked   = errors.New("provider verification is blocked")
	ErrAppliedUnverified = errors.New("APPLIED_VERIFIED lacks trusted verification evidence")
	ErrStaleMaterial     = errors.New("readset journal source material is outside validity")
)

// JournalKey is the frozen ReadSet v2 journal identity.
type JournalKey = inputartifact.JournalKey

// OwnerFence binds an append to the existing durable DecisionJob lease owner.
// ExecutionGeneration is not a context, authority, or credential epoch.
type OwnerFence struct {
	CaseID              string
	JobID               string
	WorkerID            string
	ExecutionGeneration int64
}

// AdmissionScope is the exact policy/revocation context admitted at registration.
// Every scope kind is compared with its own ID; bare epoch integers never cross
// scope kinds or replace ExecutionGeneration.
type AdmissionScope struct {
	AdmissionScopeID             string `json:"admission_scope_id"`
	SourceDependencyIndexVersion string `json:"source_dependency_index_version"`
	ContextRevocationScopeID     string `json:"context_revocation_scope_id"`
	ContextRevocationEpoch       int64  `json:"context_revocation_epoch"`
	AuthorityScopeID             string `json:"authority_scope_id"`
	AuthorityEpoch               int64  `json:"authority_epoch"`
	CredentialScopeID            string `json:"credential_scope_id"`
	CredentialEpoch              int64  `json:"credential_epoch"`
}

// RegistrationPlan is the exact canonical pre-exposure payload. Registration
// occurs before a caller may expose the material. It deliberately has no
// injected_at, verification_record_ref, or authoritative sequence, because
// those facts require an independently trusted post-apply receipt.
type RegistrationPlan struct {
	EventVersion        string                    `json:"event_version"`
	JournalKey          JournalKey                `json:"journal_key"`
	ExecutionGeneration int64                     `json:"execution_generation"`
	DeliveryID          string                    `json:"delivery_id"`
	ProviderRequestID   string                    `json:"provider_request_id"`
	Kind                string                    `json:"kind"`
	Phase               string                    `json:"phase"`
	Source              inputartifact.SourceNode  `json:"source"`
	ParentNodeIDs       []string                  `json:"parent_node_ids"`
	PermissionRef       inputartifact.ExternalRef `json:"permission_ref"`
	Producer            inputartifact.SubjectRef  `json:"producer"`
	TargetPersona       string                    `json:"target_persona"`
	AdmissionScope      AdmissionScope            `json:"admission_scope"`
}

// RegisterRequest is the sole local material-registration ingress shape.
type RegisterRequest struct {
	Owner   OwnerFence
	Event   RegistrationPlan
	Closure []inputartifact.SourceNode
}

// PreparedNode contains exact canonical source-node bytes and their raw SHA-256.
type PreparedNode struct {
	Node   inputartifact.SourceNode
	Bytes  []byte
	Digest string
}

// PreparedRegistration is the bounded, canonical transaction input. Creation
// of this value records facts only; it does not authorize provider exposure or
// assert a model read.
type PreparedRegistration struct {
	JournalID     string
	JournalKey    JournalKey
	Scope         AdmissionScope
	Event         RegistrationPlan
	EventBytes    []byte
	EventDigest   string
	RequestDigest string
	Nodes         []PreparedNode
	MaxGraphDepth int
}

// Config gates the primitive and bounds every parse/graph operation. The zero
// value and DefaultConfig are disabled. Deployment must not enable this local
// primitive for a provider path until the separate pre-exposure authority
// transaction in the #45/#47 Design Change exists.
type Config struct {
	Enabled             bool
	MaxEventBytes       int
	MaxEventMembers     int
	MaxStringBytes      int
	MaxGraphDepth       int
	MaxNodes            int
	MaxParents          int
	MaxEventsPerJournal int
	MaxJournalBytes     int64
	MaxClosureBytes     int
}

// DefaultConfig returns the fail-closed production default.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		MaxEventBytes:       32768,
		MaxEventMembers:     512,
		MaxStringBytes:      8192,
		MaxGraphDepth:       16,
		MaxNodes:            2048,
		MaxParents:          64,
		MaxEventsPerJournal: 512,
		MaxJournalBytes:     16 * 1024 * 1024,
		MaxClosureBytes:     16 * 1024 * 1024,
	}
}

// Normalized returns the bounded effective configuration while preserving the
// Enabled gate. Adapters should store this form so all append checks share the
// exact limits used during preparation.
func (c Config) Normalized() Config { return c.effective() }

func (c Config) effective() Config {
	d := DefaultConfig()
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = d.MaxEventBytes
	}
	if c.MaxEventMembers <= 0 {
		c.MaxEventMembers = d.MaxEventMembers
	}
	if c.MaxStringBytes <= 0 {
		c.MaxStringBytes = d.MaxStringBytes
	}
	if c.MaxGraphDepth <= 0 {
		c.MaxGraphDepth = d.MaxGraphDepth
	}
	if c.MaxNodes <= 0 {
		c.MaxNodes = d.MaxNodes
	}
	if c.MaxParents <= 0 {
		c.MaxParents = d.MaxParents
	}
	if c.MaxEventsPerJournal <= 0 {
		c.MaxEventsPerJournal = d.MaxEventsPerJournal
	}
	if c.MaxJournalBytes <= 0 {
		c.MaxJournalBytes = d.MaxJournalBytes
	}
	if c.MaxClosureBytes <= 0 {
		c.MaxClosureBytes = d.MaxClosureBytes
	}
	return c
}

// RegistrationState is the local durable acceptance state. It never implies
// provider execution.
type RegistrationState string

const RegistrationRegistered RegistrationState = "REGISTERED"

// ProviderState is intentionally separate from RegistrationState. This slice
// can durably classify a local delivery as pending/unknown/unverifiable, but it
// cannot create APPLIED_VERIFIED or FAILED without trusted provider evidence.
type ProviderState string

const (
	ProviderPending      ProviderState = "PENDING"
	ProviderApplied      ProviderState = "APPLIED_VERIFIED"
	ProviderFailed       ProviderState = "FAILED"
	ProviderUnknown      ProviderState = "UNKNOWN"
	ProviderUnverifiable ProviderState = "UNVERIFIABLE"
)

// JournalState is the durable per-journal lifecycle.
type JournalState string

const (
	JournalOpen        JournalState = "OPEN"
	JournalQuiescing   JournalState = "QUIESCING"
	JournalInputClosed JournalState = "INPUT_CLOSED"
)

// EventFact is the persisted, canonical registration fact. Provider fields are
// separate because durable registration precedes any possible exposure.
// DependencyEdge is one persisted child->parent reverse-index edge.
type DependencyEdge struct {
	ParentNodeID string
	ChildNodeID  string
}

type EventFact struct {
	Sequence            int64
	EventVersion        string
	DeliveryID          string
	ProviderRequestID   string
	ExecutionGeneration int64
	RegistrationState   RegistrationState
	ProviderState       ProviderState
	PayloadBytes        []byte
	PayloadDigest       string
	RegisteredAt        time.Time
	// The following fields are populated only by a future independently trusted
	// provider/C2/C3 settlement writer. They are zero for every local state.
	AuthoritySequence     int64
	VerificationRecordRef inputartifact.ExternalRef
	InjectedAt            string
}

// JournalView is a bounded read of one journal. No field proves a real model read.
type JournalView struct {
	JournalID         string
	JournalKey        JournalKey
	State             JournalState
	HighWatermark     int64
	PendingCount      int64
	Scope             AdmissionScope
	Events            []EventFact
	Nodes             []inputartifact.SourceNode
	ReverseDependents map[string][]string
}
