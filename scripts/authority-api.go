// Declaration-only shared contract for issue #45. Compiled by the offline
// checker; no production implementation, issuer, transport, signing, or wiring.
package main

import "context"

type SafeUInt uint64 // Wire range 0..2^53-1; no float/exponent encoding.
type ExactID string
type Digest string
type UTCMillis string // Strict UTC calendar time YYYY-MM-DDTHH:mm:ss.SSSZ.
type Operation string
type Outcome string

const (
	Valid    Outcome = "VALID"
	Denied   Outcome = "DENIED"
	Revoked  Outcome = "REVOKED"
	Expired  Outcome = "EXPIRED"
	Stale    Outcome = "STALE"
	Conflict Outcome = "CONFLICT"
	Unknown  Outcome = "UNKNOWN"
)

const (
	JournalAdmit     Operation = "JOURNAL_ADMIT"
	Quiesce          Operation = "QUIESCE"
	CandidateClose   Operation = "CANDIDATE_CLOSE"
	SendCommit       Operation = "SEND_COMMIT"
	PublishArtifact  Operation = "PUBLISH_ARTIFACT"
	SealReadSet      Operation = "SEAL_READSET"
	SubmitBallot     Operation = "SUBMIT_BALLOT"
	AggregateBallots Operation = "AGGREGATE_BALLOTS"
	PublishDecision  Operation = "PUBLISH_DECISION"
	PrepareAction    Operation = "PREPARE_ACTION"
	AttachWitness    Operation = "ATTACH_WITNESS"
)

type Subject struct {
	SubjectID             ExactID `json:"subject_id"`
	IdentitySourceID      ExactID `json:"identity_source_id"`
	IdentitySourceVersion string  `json:"identity_source_version"`
}
type CaseRound struct {
	CaseID ExactID  `json:"case_id"`
	Round  SafeUInt `json:"round"`
}
type ScopedEpoch struct {
	Kind    string   `json:"kind"` // CONTEXT, AUTHORITY, CREDENTIAL; no cross-kind comparison.
	ScopeID ExactID  `json:"scope_id"`
	Epoch   SafeUInt `json:"epoch"`
}
type ExactDependency struct {
	ID                  ExactID   `json:"id"`
	Digest              Digest    `json:"digest"`
	ArtifactVersion     string    `json:"artifact_version"`
	AuthorityRowVersion SafeUInt  `json:"authority_row_version"`
	Parents             []ExactID `json:"parents"`
}
type IntentBinding struct {
	ActionID                ExactID `json:"action_id"`
	IntentID                ExactID `json:"intent_id"`
	IntentDigest            Digest  `json:"intent_digest"`
	EncodedRequestDigest    Digest  `json:"encoded_request_digest"`
	LogicalOperationID      ExactID `json:"logical_operation_id"`
	BusinessParameterDigest Digest  `json:"business_parameter_digest"`
}
type VerifyRequest struct {
	Operation           Operation         `json:"operation"`
	Principal           Subject           `json:"principal"`
	CaseRound           CaseRound         `json:"case_round"`
	ExecutionGeneration SafeUInt          `json:"execution_generation"`
	ScopedEpochs        []ScopedEpoch     `json:"scoped_epochs"`
	ExactDependencies   []ExactDependency `json:"exact_dependencies"`
	ProofID             ExactID           `json:"proof_id"`
	ExactTargetID       ExactID           `json:"exact_target_id"`
	UniqueKey           ExactID           `json:"unique_key"`
	TrustedNow          UTCMillis         `json:"trusted_now"`
	SendIntent          *IntentBinding    `json:"send_intent"`
}

// Opaque server-side handle; the stored transcript binds the complete caller
// plan, derivation profile and expected state. These fields describe stored diagnostic declarations,
// never a caller-created bearer capability. No secret or target credential here.
type VerifiedToken struct {
	TokenID             ExactID       `json:"token_id"`
	Issuer              ExactID       `json:"issuer"`
	PrincipalID         ExactID       `json:"principal_id"`
	Operation           Operation     `json:"operation"`
	ExactTargetID       ExactID       `json:"exact_target_id"`
	UniqueKey           ExactID       `json:"unique_key"`
	RequestDigest       Digest        `json:"request_digest"`
	ExpectedStateDigest Digest        `json:"expected_state_digest"`
	PlanDigest          Digest        `json:"plan_digest"`
	IssuedAt            UTCMillis     `json:"issued_at"`
	ExpiresAt           UTCMillis     `json:"expires_at"`
	AuthorityVersion    SafeUInt      `json:"authority_version"`
	ExecutionGeneration SafeUInt      `json:"execution_generation"`
	ScopedEpochs        []ScopedEpoch `json:"scoped_epochs"`
}
type ExpectedState struct {
	State               string        `json:"state"`
	OwnerID             ExactID       `json:"owner_id"`
	ExecutionGeneration SafeUInt      `json:"execution_generation"`
	AuthorityVersion    SafeUInt      `json:"authority_version"`
	ScopedEpochs        []ScopedEpoch `json:"scoped_epochs"`
	GatewayID           ExactID       `json:"gateway_id"`
	GatewayGeneration   SafeUInt      `json:"gateway_generation"`
}

// PreparedObject contains only already fixed input bytes. No generated commit,
// token, sequence, time, closure/event/lineage output can be referenced here.
type PreparedObject struct {
	ID        ExactID `json:"id"`
	Digest    Digest  `json:"digest"`
	ObjectRef ExactID `json:"object_ref"`
}
type HighWatermarkPlan struct {
	JournalID ExactID  `json:"journal_id"`
	Expected  SafeUInt `json:"expected"`
	Next      SafeUInt `json:"next"`
}
type ProviderSettlementPlan struct {
	RequestID       ExactID        `json:"request_id"`
	ExpectedStatus  string         `json:"expected_status"`
	NextStatus      string         `json:"next_status"`
	VerificationRef PreparedObject `json:"verification_ref"`
}
type PendingTransitionPlan struct {
	JournalID ExactID  `json:"journal_id"`
	Expected  SafeUInt `json:"expected"`
	Next      SafeUInt `json:"next"`
}
type JournalAdmissionPlan struct {
	AdmissionInput       PreparedObject         `json:"admission_input"`
	JournalEventInput    PreparedObject         `json:"journal_event_input"`
	DependencyEdgeInputs []PreparedObject       `json:"dependency_edge_inputs"`
	HighWatermark        HighWatermarkPlan      `json:"high_watermark"`
	ProviderSettlement   ProviderSettlementPlan `json:"provider_settlement"`
	PendingTransition    PendingTransitionPlan  `json:"pending_transition"`
}
type StateTransitionPlan struct {
	ObjectID      ExactID `json:"object_id"`
	ExpectedState string  `json:"expected_state"`
	NextState     string  `json:"next_state"`
}
type QuiescencePlan struct {
	FenceID  ExactID                `json:"fence_id"`
	Round    StateTransitionPlan    `json:"round"`
	Journals [3]StateTransitionPlan `json:"journals"`
}
type JournalClosurePlan struct {
	Role                  string   `json:"role"`
	JournalID             ExactID  `json:"journal_id"`
	ExpectedHighWatermark SafeUInt `json:"expected_high_watermark"`
	CloseHighWatermark    SafeUInt `json:"close_high_watermark"`
	ExpectedPending       SafeUInt `json:"expected_pending"`
}
type CandidateClosurePlan struct {
	Candidate     PreparedObject        `json:"candidate"`
	FenceID       ExactID               `json:"fence_id"`
	Receipts      [3]PreparedObject     `json:"receipts"`
	Confirmations [3]PreparedObject     `json:"confirmations"`
	Journals      [3]JournalClosurePlan `json:"journals"`
	Round         StateTransitionPlan   `json:"round"`
}
type ActionTransitionPlan struct {
	ExpectedVersion   SafeUInt `json:"expected_version"`
	NextVersion       SafeUInt `json:"next_version"`
	ActionID          ExactID  `json:"action_id"`
	ExpectedState     string   `json:"expected_state"`
	NextState         string   `json:"next_state"`
	GatewayID         ExactID  `json:"gateway_id"`
	GatewayGeneration SafeUInt `json:"gateway_generation"`
}
type QuotaTransitionPlan struct {
	ExpectedVersion   SafeUInt `json:"expected_version"`
	NextVersion       SafeUInt `json:"next_version"`
	QuotaID           ExactID  `json:"quota_id"`
	ExpectedRemaining SafeUInt `json:"expected_remaining"`
	Amount            SafeUInt `json:"amount"`
	NextRemaining     SafeUInt `json:"next_remaining"`
}
type LogicalOperationPlan struct {
	LogicalOperationID      ExactID `json:"logical_operation_id"`
	BusinessParameterDigest Digest  `json:"business_parameter_digest"`
	Mode                    string  `json:"mode"` // SEND only VERIFY_EXISTING; PREPARE owns CREATE_IF_ABSENT.
}
type SendPlan struct {
	Intent           PreparedObject       `json:"intent"`
	ActionTransition ActionTransitionPlan `json:"action_transition"`
	QuotaTransition  QuotaTransitionPlan  `json:"quota_transition"`
	LogicalBinding   LogicalOperationPlan `json:"logical_binding"`
}

// CompleteWritePlan is an acyclic pre-token proposal. Exactly one variant is
// non-nil. MutationSet and DerivationProfile bind WHICH transaction-derived rows
// and algorithms must run; they never contain finalized output bytes/digests.
type CompleteWritePlan struct {
	PlanVersion       string                `json:"plan_version"`
	DerivationProfile string                `json:"derivation_profile"`
	Operation         Operation             `json:"operation"`
	MutationSet       []string              `json:"mutation_set"`
	Admission         *JournalAdmissionPlan `json:"admission"`
	Quiescence        *QuiescencePlan       `json:"quiescence"`
	Closure           *CandidateClosurePlan `json:"closure"`
	Send              *SendPlan             `json:"send"`
	OutboxKinds       []string              `json:"outbox_kinds"`
}
type CommitRequest struct {
	UniqueKey        ExactID
	ExactTargetID    ExactID
	CommitToken      ExactID
	ExpectedState    ExpectedState
	CompleteWriteSet CompleteWritePlan // caller plan, not finalized/generated rows
}

// Allocation is produced after verification/locking. All finalized rows bind
// plan/request/expected digests + this allocation; result rows do not hash lineage.
type TransactionAllocation struct {
	TokenID, CommitID ExactID
	AuthoritySequence SafeUInt
	CommittedAt       UTCMillis
}
type FinalizedResultRef struct {
	Kind   string
	ID     ExactID
	Digest Digest
}
type AuditReadRequest struct {
	Requester                                   Subject
	CaseRound                                   CaseRound
	UniqueKey, ExactTargetID, HistoricalTokenID ExactID
}
type AuditReadContract interface {
	ReadOriginal(context.Context, AuditReadRequest) CommitResult
}
type Failure struct {
	Outcome   Outcome // Any non-VALID code; UNKNOWN never implies no commit.
	Reason    string
	Retryable bool // Advice only; never permission to mutate/replay a send.
}
type VerifyResult struct {
	Outcome Outcome
	Token   *VerifiedToken // Present iff VALID. Authorizes no mutation by itself.
	Failure *Failure       // Present iff non-VALID.
}
type CommitLineage struct {
	DerivationProfile                              string
	CommitID, UniqueKey, ExactTargetID, TokenID    ExactID
	RequestDigest, PlanDigest, ExpectedStateDigest Digest
	AuthoritySequence                              SafeUInt
	CommittedAt                                    UTCMillis
	ResultRefs                                     []FinalizedResultRef
}
type CommitResult struct {
	Outcome     Outcome
	Disposition string         // NEW or ORIGINAL iff VALID; ORIGINAL is read-only history.
	Lineage     *CommitLineage // Present iff VALID, immutable whole-commit lineage.
	Failure     *Failure
}

// VerifyAuthority receives an acyclic write plan and expected-state proposal so the token
// cannot be moved to different writes after verification. Authentication and
// TrustedNow are service-injected, never accepted from model/caller JSON.
type AuthorityContract interface {
	VerifyAuthority(context.Context, VerifyRequest, ExpectedState, CompleteWritePlan) VerifyResult
	CommitIfCurrent(context.Context, CommitRequest) CommitResult
}

// No struct implements AuthorityContract in this design slice.
