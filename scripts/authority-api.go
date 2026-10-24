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
	ID      ExactID   `json:"id"`
	Digest  Digest    `json:"digest"`
	Version SafeUInt  `json:"version"`
	Parents []ExactID `json:"parents"`
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
}

// Opaque server-side handle; the stored transcript also binds the entire write
// set and expected state. These fields describe stored diagnostic declarations,
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
	WriteSetDigest      Digest        `json:"write_set_digest"`
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

// ImmutableRow is a validated exact reference to prepared canonical bytes;
// consumers must load/revalidate those bytes, not trust a digest declaration.
type ImmutableRow struct {
	ID        ExactID
	Digest    Digest
	ObjectRef ExactID
}
type JournalAdmissionWrites struct {
	Admission, JournalEvent ImmutableRow
	DependencyEdges         []ImmutableRow
}
type QuiescenceWrites struct {
	Fence, RoundState ImmutableRow
	Journals          [3]ImmutableRow // fixed balthasar, casper, melchior slots
}
type CandidateClosureWrites struct {
	Candidate, Closure, EvidenceClosed ImmutableRow
	Journals                           [3]ImmutableRow
}
type SendWrites struct {
	SendRecord, QuotaConsumption ImmutableRow
}

// Exactly one variant is present and agrees with Operation. All four variants
// additionally commit immutable lineage, token consumption, unique binding and
// outbox together. Unsupported operations cannot use a generic table-mutation API.
type CompleteWriteSet struct {
	Operation     Operation
	Admission     *JournalAdmissionWrites
	Quiescence    *QuiescenceWrites
	Closure       *CandidateClosureWrites
	Send          *SendWrites
	Lineage       ImmutableRow
	UniqueBinding ImmutableRow
	ConsumedToken ImmutableRow
	Outbox        []ImmutableRow
}
type CommitRequest struct {
	UniqueKey        ExactID
	ExactTargetID    ExactID
	CommitToken      ExactID
	ExpectedState    ExpectedState
	CompleteWriteSet CompleteWriteSet
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
	CommitID, UniqueKey, ExactTargetID, TokenID        ExactID
	RequestDigest, WriteSetDigest, ExpectedStateDigest Digest
	AuthoritySequence                                  SafeUInt
	CommittedAt                                        UTCMillis
	ResultRefs                                         []ImmutableRow
}
type CommitResult struct {
	Outcome     Outcome
	Disposition string         // NEW or ORIGINAL iff VALID; ORIGINAL is read-only history.
	Lineage     *CommitLineage // Present iff VALID, immutable whole-commit lineage.
	Failure     *Failure
}

// VerifyAuthority receives write-set and expected-state proposals so the token
// cannot be moved to different writes after verification. Authentication and
// TrustedNow are service-injected, never accepted from model/caller JSON.
type AuthorityContract interface {
	VerifyAuthority(context.Context, VerifyRequest, ExpectedState, CompleteWriteSet) VerifyResult
	CommitIfCurrent(context.Context, CommitRequest) CommitResult
}

// No struct implements AuthorityContract in this design slice.
