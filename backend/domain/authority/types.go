// Package authority implements the C2 authenticated-authority slice for issue
// #46. It verifies a versioned Ed25519 proof envelope against live trust state.
// A VALID result means local authentication only; it is not a commit token,
// witness, journal closure, or send permission.
package authority

const (
	MaxDelegationHops = 16
	MaxOwnerApprovals = 64
	MaxPolicyOwners   = 64
)

const (
	ContractVersion           = "magi-authority-c2/1"
	ProofVersion              = "magi-authority-proof/1"
	DelegationVersion         = "magi-authority-delegation/1"
	ConsentVersion            = "magi-authority-consent/1"
	BootstrapVersion          = "magi-authority-bootstrap/1"
	SignatureAlgorithmEd25519 = "ed25519"
)

type Outcome string

const (
	OutcomeValid    Outcome = "VALID"
	OutcomeDenied   Outcome = "DENIED"
	OutcomeRevoked  Outcome = "REVOKED"
	OutcomeExpired  Outcome = "EXPIRED"
	OutcomeStale    Outcome = "STALE"
	OutcomeConflict Outcome = "CONFLICT"
	OutcomeUnknown  Outcome = "UNKNOWN"
	OutcomeBlocked  Outcome = "BLOCKED"
)

// Result is a typed verification outcome. Only VALID sets
// LocalAuthenticationOnly. No outcome authorizes a commit, witness, journal
// closure, EVIDENCE_CLOSED, or send.
type Result struct {
	Outcome                 Outcome
	Code                    string
	Detail                  string
	LocalAuthenticationOnly bool
	CommitEligible          bool
	MutationAuthorized      bool
}

type Subject struct {
	SubjectID             string `json:"subject_id"`
	IdentitySourceID      string `json:"identity_source_id"`
	IdentitySourceVersion string `json:"identity_source_version"`
}

type CredentialRef struct {
	RootIncarnation   string `json:"root_incarnation"`
	CredentialID      string `json:"credential_id"`
	CredentialVersion uint64 `json:"credential_version"`
}

type CredentialStatus string

const (
	CredentialActive  CredentialStatus = "ACTIVE"
	CredentialRetired CredentialStatus = "RETIRED"
	CredentialRevoked CredentialStatus = "REVOKED"
)

type Credential struct {
	Ref       CredentialRef    `json:"ref"`
	Subject   Subject          `json:"subject"`
	Algorithm string           `json:"algorithm"`
	PublicKey string           `json:"public_key"`
	NotBefore string           `json:"not_before"`
	ExpiresAt string           `json:"expires_at"`
	Status    CredentialStatus `json:"status"`
	Scope     ScopeKey         `json:"scope"`
}

type ScopeKind string

const (
	ScopeAuthority  ScopeKind = "AUTHORITY"
	ScopeContext    ScopeKind = "CONTEXT"
	ScopeCredential ScopeKind = "CREDENTIAL"
)

type ScopeKey struct {
	Kind    ScopeKind `json:"kind"`
	ScopeID string    `json:"scope_id"`
}

type ScopedEpoch struct {
	Kind    ScopeKind `json:"kind"`
	ScopeID string    `json:"scope_id"`
	Epoch   uint64    `json:"epoch"`
}

type EpochState struct {
	Scope      ScopeKey `json:"scope"`
	Epoch      uint64   `json:"epoch"`
	Revoked    bool     `json:"revoked"`
	RowVersion uint64   `json:"row_version"`
}

type RootStatus string

const (
	RootActive  RootStatus = "ACTIVE"
	RootRetired RootStatus = "RETIRED"
)

type OwnerMode string

const (
	OwnerAll            OwnerMode = "ALL"
	OwnerThreshold      OwnerMode = "THRESHOLD"
	OwnerSingleDelegate OwnerMode = "SINGLE_DELEGATE"
)

type PolicyStatus string

const (
	PolicyActive  PolicyStatus = "ACTIVE"
	PolicyRetired PolicyStatus = "RETIRED"
)

type OwnerPolicy struct {
	RootIncarnation string       `json:"root_incarnation"`
	PolicyID        string       `json:"policy_id"`
	PolicyVersion   string       `json:"policy_version"`
	Mode            OwnerMode    `json:"mode"`
	Threshold       uint64       `json:"threshold"`
	Owners          []Subject    `json:"owners"`
	Resources       []string     `json:"resources"`
	Actions         []string     `json:"actions"`
	Tenants         []string     `json:"tenants"`
	Operations      []string     `json:"operations"`
	NotBefore       string       `json:"not_before"`
	ExpiresAt       string       `json:"expires_at"`
	Status          PolicyStatus `json:"status"`
}

type DelegationBody struct {
	Version          string        `json:"version"`
	DelegationID     string        `json:"delegation_id"`
	TrustDomain      string        `json:"trust_domain"`
	RootIncarnation  string        `json:"root_incarnation"`
	Issuer           Subject       `json:"issuer"`
	IssuerCredential CredentialRef `json:"issuer_credential"`
	Delegate         Subject       `json:"delegate"`
	Resources        []string      `json:"resources"`
	Actions          []string      `json:"actions"`
	Tenants          []string      `json:"tenants"`
	Operations       []string      `json:"operations"`
	NotBefore        string        `json:"not_before"`
	ExpiresAt        string        `json:"expires_at"`
	Delegable        bool          `json:"delegable"`
}

type SignedDelegation struct {
	Body      DelegationBody `json:"body"`
	Signature string         `json:"signature"`
}

type ConsentBody struct {
	Version             string        `json:"version"`
	ConsentID           string        `json:"consent_id"`
	TrustDomain         string        `json:"trust_domain"`
	RootIncarnation     string        `json:"root_incarnation"`
	PolicyID            string        `json:"policy_id"`
	PolicyVersion       string        `json:"policy_version"`
	DelegatedSubject    Subject       `json:"delegated_subject"`
	Resource            string        `json:"resource"`
	Action              string        `json:"action"`
	Tenant              string        `json:"tenant"`
	Operation           string        `json:"operation"`
	CaseID              string        `json:"case_id"`
	Round               uint64        `json:"round"`
	ExecutionGeneration uint64        `json:"execution_generation"`
	Scopes              []ScopedEpoch `json:"scopes"`
	NotBefore           string        `json:"not_before"`
	ExpiresAt           string        `json:"expires_at"`
}

type ConsentApproval struct {
	Owner     Subject       `json:"owner"`
	Signer    CredentialRef `json:"signer"`
	Signature string        `json:"signature"`
}

type SignedConsent struct {
	Body      ConsentBody       `json:"body"`
	Approvals []ConsentApproval `json:"approvals"`
}

type Proof struct {
	Version             string             `json:"version"`
	TrustDomain         string             `json:"trust_domain"`
	RootIncarnation     string             `json:"root_incarnation"`
	Issuer              Subject            `json:"issuer"`
	Subject             Subject            `json:"subject"`
	Resource            string             `json:"resource"`
	Action              string             `json:"action"`
	Tenant              string             `json:"tenant"`
	Operation           string             `json:"operation"`
	CaseID              string             `json:"case_id"`
	Round               uint64             `json:"round"`
	ExecutionGeneration uint64             `json:"execution_generation"`
	Scopes              []ScopedEpoch      `json:"scopes"`
	IssuedAt            string             `json:"issued_at"`
	ExpiresAt           string             `json:"expires_at"`
	Signer              CredentialRef      `json:"signer"`
	Delegations         []SignedDelegation `json:"delegations"`
	Consent             SignedConsent      `json:"consent"`
}

type SignedProof struct {
	Proof     Proof  `json:"proof"`
	ProofID   string `json:"proof_id"`
	Signature string `json:"signature"`
}

type VerificationRequest struct {
	TrustDomain         string        `json:"trust_domain"`
	ProofID             string        `json:"proof_id"`
	Subject             Subject       `json:"subject"`
	Resource            string        `json:"resource"`
	Action              string        `json:"action"`
	Tenant              string        `json:"tenant"`
	Operation           string        `json:"operation"`
	CaseID              string        `json:"case_id"`
	Round               uint64        `json:"round"`
	ExecutionGeneration uint64        `json:"execution_generation"`
	Scopes              []ScopedEpoch `json:"scopes"`
}

type AuthorityGrant struct {
	Resources  []string `json:"resources"`
	Actions    []string `json:"actions"`
	Tenants    []string `json:"tenants"`
	Operations []string `json:"operations"`
}

type TrustState struct {
	TrustDomain           string
	RootIncarnation       string
	RootFingerprint       string
	RootStatus            RootStatus
	IdentitySourceID      string
	IdentitySourceVersion string
	GovernanceSubject     Subject
	RootGrant             AuthorityGrant
	TrustedNow            string
	Credentials           []Credential
	Policies              []OwnerPolicy
	Epochs                []EpochState
}

type TrustBootstrap struct {
	Version               string         `json:"version"`
	TrustDomain           string         `json:"trust_domain"`
	RootIncarnation       string         `json:"root_incarnation"`
	IdentitySourceID      string         `json:"identity_source_id"`
	IdentitySourceVersion string         `json:"identity_source_version"`
	GovernanceSubject     Subject        `json:"governance_subject"`
	RootGrant             AuthorityGrant `json:"root_grant"`
	Credentials           []Credential   `json:"credentials"`
	OwnerPolicies         []OwnerPolicy  `json:"owner_policies"`
	InitialScopes         []ScopedEpoch  `json:"initial_scopes"`
}

type BootstrapEnvelope struct {
	Bootstrap  TrustBootstrap `json:"bootstrap"`
	KeyID      string         `json:"key_id"`
	KeyVersion uint64         `json:"key_version"`
	Algorithm  string         `json:"algorithm"`
	Signature  string         `json:"signature"`
}

type BootstrapTrust struct {
	IdentitySourceID      string `json:"identity_source_id"`
	IdentitySourceVersion string `json:"identity_source_version"`
	RecoveryKeyID         string `json:"recovery_key_id"`
	RecoveryKeyVersion    uint64 `json:"recovery_key_version"`
	Algorithm             string `json:"algorithm"`
	PublicKey             string `json:"public_key"`
	TrustedNow            string `json:"trusted_now"`
}

const (
	RevocationCommandVersion         = "magi-authority-revocation-command/1"
	CredentialRotationCommandVersion = "magi-authority-credential-rotation-command/1"
)

type RevocationCommandBody struct {
	Version            string   `json:"version"`
	CommandID          string   `json:"command_id"`
	TrustDomain        string   `json:"trust_domain"`
	RootIncarnation    string   `json:"root_incarnation"`
	Scope              ScopeKey `json:"scope"`
	ExpectedEpoch      uint64   `json:"expected_epoch"`
	ExpectedRowVersion uint64   `json:"expected_row_version"`
	Reason             string   `json:"reason"`
	IssuedAt           string   `json:"issued_at"`
	ExpiresAt          string   `json:"expires_at"`
}

type SignedRevocationCommand struct {
	Body      RevocationCommandBody `json:"body"`
	Signer    CredentialRef         `json:"signer"`
	Signature string                `json:"signature"`
}

type CredentialRotationCommandBody struct {
	Version            string        `json:"version"`
	CommandID          string        `json:"command_id"`
	TrustDomain        string        `json:"trust_domain"`
	RootIncarnation    string        `json:"root_incarnation"`
	OldCredential      CredentialRef `json:"old_credential"`
	NewCredential      Credential    `json:"new_credential"`
	ExpectedEpoch      uint64        `json:"expected_epoch"`
	ExpectedRowVersion uint64        `json:"expected_row_version"`
	Reason             string        `json:"reason"`
	IssuedAt           string        `json:"issued_at"`
	ExpiresAt          string        `json:"expires_at"`
}

type SignedCredentialRotationCommand struct {
	Body      CredentialRotationCommandBody `json:"body"`
	Signer    CredentialRef                 `json:"signer"`
	Signature string                        `json:"signature"`
}
