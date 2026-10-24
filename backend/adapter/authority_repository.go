package magi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
	"gorm.io/gorm"
)

const (
	authorityOwnerPolicyEvidenceDomain = "magi:authority-owner-policy-evidence:v1\n"
	authorityDelegationEvidenceDomain  = "magi:authority-delegation-evidence:v1\n"
	authorityConsentEvidenceDomain     = "magi:authority-consent-evidence:v1\n"
	authorityScopeEvidenceDomain       = "magi:authority-proof-scope-evidence:v1\n"
	authorityCommandEvidenceDomain     = "magi:authority-command-evidence:v1\n"
)

// AuthorityRepositoryConfig gates C2 persistence. The zero value is disabled.
// RecoveryTrust must be injected by a separately authorized deployment
// bootstrap; this package has no default recovery key or governance root.
type AuthorityRepositoryConfig struct {
	Enabled       bool
	RecoveryTrust authority.BootstrapTrust
}

// AuthorityRepository is the MySQL-only C2 persistence boundary. Its mutation
// methods are internal control-plane operations; production wiring is absent
// and capability-disabled by default.
type AuthorityRepository struct {
	db  *gorm.DB
	cfg AuthorityRepositoryConfig
}

func NewAuthorityRepository(db *gorm.DB, cfg AuthorityRepositoryConfig) *AuthorityRepository {
	return &AuthorityRepository{db: db, cfg: cfg}
}

type authorityRootRow struct {
	TrustDomain           string
	RootIncarnation       string
	RootFingerprint       string
	BootstrapVersion      string
	IdentitySourceID      string
	IdentitySourceVersion string
	GovernanceSubjectID   string
	Status                string
	RowVersion            uint64
	RawBootstrap          []byte
}

type authorityProofRow struct {
	TrustDomain     string
	ProofID         string
	ProofVersion    string
	ProofDigest     string
	RootFingerprint string
	RawProof        []byte
}

type authorityCredentialRow struct {
	TrustDomain           string
	RootIncarnation       string
	CredentialID          string
	CredentialVersion     uint64
	SubjectID             string
	IdentitySourceID      string
	IdentitySourceVersion string
	Algorithm             string
	PublicKey             []byte
	NotBefore             time.Time
	ExpiresAt             time.Time
	Status                string
	CredentialScopeID     string
	RawCredential         []byte
}

type authorityPolicyRow struct {
	TrustDomain     string
	RootIncarnation string
	PolicyID        string
	PolicyVersion   string
	Status          string
	PolicyDigest    string
	RawPolicy       []byte
}

type authorityCommandRow struct {
	CommandKind             string
	CommandDigest           string
	RawCommand              []byte
	ResultScopeKind         string
	ResultScopeID           string
	ResultEpoch             uint64
	ResultRowVersion        uint64
	ResultRevoked           bool
	ResultCredentialID      string
	ResultCredentialVersion uint64
	ResultCredentialRaw     []byte
}

type authorityEpochRow struct {
	ScopeKind  string
	ScopeID    string
	Epoch      uint64
	Revoked    bool
	RowVersion uint64
}

func (r *AuthorityRepository) capabilityResult() *authority.Result {
	if r == nil || r.db == nil {
		result := authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_UNAVAILABLE"}
		return &result
	}
	if !r.cfg.Enabled {
		result := authority.Result{Outcome: authority.OutcomeDenied, Code: "AUTHORITY_CAPABILITY_DISABLED"}
		return &result
	}
	return nil
}

func (r *AuthorityRepository) begin(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func lockActiveAuthorityRoot(tx *gorm.DB, trustDomain string) (authorityRootRow, authority.Result) {
	var row authorityRootRow
	err := tx.Raw(`SELECT trust_domain, root_incarnation, root_fingerprint, bootstrap_version,
			identity_source_id, identity_source_version, governance_subject_id, status,
			row_version, raw_bootstrap
		FROM magi_authority_root
		WHERE trust_domain = ? AND active_slot = 1 FOR UPDATE`, trustDomain).
		Row().Scan(&row.TrustDomain, &row.RootIncarnation, &row.RootFingerprint,
		&row.BootstrapVersion, &row.IdentitySourceID, &row.IdentitySourceVersion,
		&row.GovernanceSubjectID, &row.Status, &row.RowVersion, &row.RawBootstrap)
	if errors.Is(err, sql.ErrNoRows) {
		return authorityRootRow{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "TRUST_ROOT_MISSING"}
	}
	if err != nil {
		return authorityRootRow{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return row, authority.Result{}
}

func authorityRootStatusResult(status string) authority.Result {
	switch status {
	case "ACTIVE":
		return authority.Result{}
	case "RETIRED":
		return authority.Result{Outcome: authority.OutcomeRevoked, Code: "TRUST_ROOT_RETIRED"}
	case "REVOKED":
		return authority.Result{Outcome: authority.OutcomeRevoked, Code: "TRUST_ROOT_REVOKED"}
	default:
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "TRUST_ROOT_STATUS_UNKNOWN"}
	}
}

func lockAuthorityProof(tx *gorm.DB, trustDomain, proofID string) (*authorityProofRow, authority.Result) {
	var row authorityProofRow
	err := tx.Raw(`SELECT trust_domain, proof_id, proof_version, proof_digest,
			root_fingerprint, raw_proof
		FROM magi_authority_proof WHERE trust_domain = ? AND proof_id = ? FOR UPDATE`,
		trustDomain, proofID).
		Row().Scan(&row.TrustDomain, &row.ProofID, &row.ProofVersion, &row.ProofDigest,
		&row.RootFingerprint, &row.RawProof)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func authorityDBTime(tx *gorm.DB) (time.Time, authority.Result) {
	var now time.Time
	if err := tx.Raw(`SELECT UTC_TIMESTAMP(3)`).Row().Scan(&now); err != nil {
		return time.Time{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "TRUSTED_TIME_UNAVAILABLE", Detail: err.Error()}
	}
	return now.UTC(), authority.Result{}
}

func authorityFormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func (r *AuthorityRepository) trustWithTime(now time.Time) authority.BootstrapTrust {
	trust := r.cfg.RecoveryTrust
	trust.TrustedNow = authorityFormatTime(now)
	return trust
}

// BootstrapRoot performs the one-time out-of-band root installation. It is
// rejected after any active root exists for the trust domain.
func (r *AuthorityRepository) BootstrapRoot(ctx context.Context, raw []byte) (authority.TrustBootstrap, string, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return authority.TrustBootstrap{}, "", *result
	}
	tx := r.begin(ctx)
	if tx.Error != nil {
		return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: tx.Error.Error()}
	}
	defer func() { _ = tx.Rollback().Error }()
	// Authenticate the envelope before changing state. The committed row is
	// re-authenticated again under the transaction below.
	preflight, _, preflightResult := authority.AuthenticateBootstrap(raw, r.trustWithTime(time.Now().UTC()))
	if preflightResult.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return authority.TrustBootstrap{}, "", preflightResult
	}
	// Root installation is serialized by the active-root row where one exists.
	// Concurrent first installs are additionally constrained by the unique
	// active_slot key; no authority sequence is created by C2.
	if existing, result := lockActiveAuthorityRoot(tx, preflight.TrustDomain); result.Outcome == "" {
		if statusResult := authorityRootStatusResult(existing.Status); statusResult.Outcome != "" {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", statusResult
		}
		now, timeResult := authorityDBTime(tx)
		if timeResult.Outcome != "" {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", timeResult
		}
		if !bytes.Equal(existing.RawBootstrap, raw) {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeConflict, Code: "TRUST_ROOT_ALREADY_INSTALLED"}
		}
		body, fingerprint, replayResult := authority.AuthenticateBootstrap(raw, r.trustWithTime(now))
		if replayResult.Outcome != authority.OutcomeValid || fingerprint != existing.RootFingerprint {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeConflict, Code: "TRUST_ROOT_FINGERPRINT_CONFLICT"}
		}
		if err := tx.Commit().Error; err != nil {
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
		}
		return body, fingerprint, authority.Result{Outcome: authority.OutcomeValid, Code: "TRUST_ROOT_ALREADY_INSTALLED"}
	} else if result.Code != "TRUST_ROOT_MISSING" {
		_ = tx.Rollback().Error
		return authority.TrustBootstrap{}, "", result
	}
	now, result := authorityDBTime(tx)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.TrustBootstrap{}, "", result
	}
	body, fingerprint, authResult := authority.AuthenticateBootstrap(raw, r.trustWithTime(now))
	if authResult.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return authority.TrustBootstrap{}, "", authResult
	}
	if err := tx.Exec(`INSERT INTO magi_authority_root
			(trust_domain, root_incarnation, root_fingerprint, bootstrap_version,
			 identity_source_id, identity_source_version, governance_subject_id,
			 status, active_slot, row_version, installed_at, raw_bootstrap)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'ACTIVE', 1, 1, ?, ?)`,
		body.TrustDomain, body.RootIncarnation, fingerprint, body.Version,
		body.IdentitySourceID, body.IdentitySourceVersion, body.GovernanceSubject.SubjectID,
		now, raw).Error; err != nil {
		_ = tx.Rollback().Error
		return authority.TrustBootstrap{}, "", authorityDuplicateOrStoreError(err, "TRUST_ROOT_ALREADY_INSTALLED")
	}
	for _, credential := range body.Credentials {
		credentialRaw, err := canonicalStructBytes(credential)
		if err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeDenied, Code: "BOOTSTRAP_CREDENTIAL_INVALID", Detail: err.Error()}
		}
		publicKey, err := base64.StdEncoding.DecodeString(credential.PublicKey)
		if err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeDenied, Code: "BOOTSTRAP_CREDENTIAL_INVALID", Detail: err.Error()}
		}
		notBefore, _ := time.Parse("2006-01-02T15:04:05.000Z", credential.NotBefore)
		expiresAt, _ := time.Parse("2006-01-02T15:04:05.000Z", credential.ExpiresAt)
		if err := tx.Exec(`INSERT INTO magi_authority_credential
				(trust_domain, root_incarnation, credential_id, credential_version,
				 subject_id, identity_source_id, identity_source_version, algorithm,
				 public_key, not_before, expires_at, status, credential_scope_id,
				 created_at, raw_credential)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			body.TrustDomain, credential.Ref.RootIncarnation, credential.Ref.CredentialID, credential.Ref.CredentialVersion,
			credential.Subject.SubjectID, credential.Subject.IdentitySourceID, credential.Subject.IdentitySourceVersion,
			credential.Algorithm, publicKey, notBefore, expiresAt, credential.Status,
			credential.Scope.ScopeID, now, credentialRaw).Error; err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
		}
	}
	for _, policy := range body.OwnerPolicies {
		policyRaw, err := canonicalStructBytes(policy)
		if err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeDenied, Code: "BOOTSTRAP_OWNER_POLICY_INVALID", Detail: err.Error()}
		}
		digest := authorityEvidenceDigest(authorityOwnerPolicyEvidenceDomain, policyRaw)
		if err := tx.Exec(`INSERT INTO magi_authority_owner_policy
				(trust_domain, root_incarnation, policy_id, policy_version, mode,
				 approval_threshold, owner_count, status, policy_digest, raw_policy, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			body.TrustDomain, body.RootIncarnation, policy.PolicyID, policy.PolicyVersion, policy.Mode,
			policy.Threshold, len(policy.Owners), policy.Status, digest, policyRaw, now).Error; err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
		}
	}
	for _, scope := range body.InitialScopes {
		if err := tx.Exec(`INSERT INTO magi_authority_epoch
				(trust_domain, scope_kind, scope_id, epoch, revoked, row_version, reason, updated_at)
				VALUES (?, ?, ?, ?, 0, 1, 'BOOTSTRAP', ?)`,
			body.TrustDomain, scope.Kind, scope.ScopeID, scope.Epoch, now).Error; err != nil {
			_ = tx.Rollback().Error
			return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return authority.TrustBootstrap{}, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
	}
	return body, fingerprint, authority.Result{Outcome: authority.OutcomeValid, Code: "TRUST_ROOT_INSTALLED"}
}

func (r *AuthorityRepository) ReadRoot(ctx context.Context, trustDomain string) ([]byte, string, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return nil, "", *result
	}
	var raw []byte
	var fingerprint, status string
	if err := r.db.WithContext(ctx).Raw(`SELECT raw_bootstrap, root_fingerprint, status
		FROM magi_authority_root WHERE trust_domain = ? AND active_slot = 1`, trustDomain).
		Row().Scan(&raw, &fingerprint, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "TRUST_ROOT_MISSING"}
		}
		return nil, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if result := authorityRootStatusResult(status); result.Outcome != "" {
		return nil, "", result
	}
	now, result := authorityDBTime(r.db.WithContext(ctx))
	if result.Outcome != "" {
		return nil, "", result
	}
	_, checked, check := authority.ReadBootstrap(raw, fingerprint, r.trustWithTime(now))
	if check.Outcome != authority.OutcomeValid {
		return nil, "", check
	}
	return append([]byte(nil), raw...), checked, authority.Result{Outcome: authority.OutcomeValid, Code: "TRUST_ROOT_READBACK_VALID"}
}

func (r *AuthorityRepository) ReadProof(ctx context.Context, trustDomain, proofID string) ([]byte, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return nil, *result
	}
	var raw []byte
	var storedDigest, storedVersion, storedRootFingerprint string
	if err := r.db.WithContext(ctx).Raw(`SELECT raw_proof, proof_digest, proof_version, root_fingerprint
		FROM magi_authority_proof WHERE trust_domain = ? AND proof_id = ?`, trustDomain, proofID).
		Row().Scan(&raw, &storedDigest, &storedVersion, &storedRootFingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "PROOF_UNKNOWN"}
		}
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	proof, result := authority.ParseSignedProof(raw, proofID)
	if result.Outcome != authority.OutcomeValid {
		return nil, result
	}
	digest, _, err := authority.ProofDigest(proof.Proof)
	if err != nil || digest != storedDigest {
		return nil, authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_DIGEST_MISMATCH", Detail: "stored proof digest differs from canonical proof bytes"}
	}
	if storedVersion != proof.Proof.Version || strings.TrimSpace(storedRootFingerprint) == "" {
		return nil, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_STORAGE_CONFLICT", Detail: "proof metadata differs from canonical evidence"}
	}
	return append([]byte(nil), raw...), authority.Result{Outcome: authority.OutcomeValid, Code: "PROOF_READBACK_VALID"}
}

func (r *AuthorityRepository) VerifyProof(ctx context.Context, request authority.VerificationRequest) authority.Result {
	if result := r.capabilityResult(); result != nil {
		return *result
	}
	tx := r.begin(ctx)
	if tx.Error != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: tx.Error.Error()}
	}
	defer func() { _ = tx.Rollback().Error }()
	result := r.verifyProofInTx(tx, request)
	if result.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return result
	}
	if err := tx.Commit().Error; err != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
	}
	return result
}

func (r *AuthorityRepository) verifyProofInTx(tx *gorm.DB, request authority.VerificationRequest) authority.Result {
	root, result := lockActiveAuthorityRoot(tx, request.TrustDomain)
	if result.Outcome != "" {
		return result
	}
	if result := authorityRootStatusResult(root.Status); result.Outcome != "" {
		return result
	}
	proofRow, result := lockAuthorityProof(tx, request.TrustDomain, request.ProofID)
	if result.Outcome != "" {
		return result
	}
	if proofRow == nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "PROOF_UNKNOWN"}
	}
	proof, parseResult := authority.ParseSignedProof(proofRow.RawProof, request.ProofID)
	if parseResult.Outcome != authority.OutcomeValid {
		return parseResult
	}
	if result := authority.ValidateProofStructureLimits(proof); result.Outcome != "" {
		return result
	}
	digest, _, err := authority.ProofDigest(proof.Proof)
	if err != nil || digest != proofRow.ProofDigest {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_DIGEST_MISMATCH", Detail: "stored proof digest differs from canonical proof bytes"}
	}
	if proofRow.ProofVersion != proof.Proof.Version || proofRow.RootFingerprint != root.RootFingerprint {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_STORAGE_CONFLICT", Detail: "proof metadata differs from its canonical evidence or pinned root"}
	}
	state, result := r.lockTrustState(tx, root, proof, true)
	if result.Outcome != "" {
		return result
	}
	now, result := authorityDBTime(tx)
	if result.Outcome != "" {
		return result
	}
	body, fingerprint, rootResult := authority.ReadBootstrap(root.RawBootstrap, root.RootFingerprint, r.trustWithTime(now))
	if rootResult.Outcome != authority.OutcomeValid {
		return rootResult
	}
	state.TrustDomain = body.TrustDomain
	state.RootIncarnation = body.RootIncarnation
	state.RootFingerprint = fingerprint
	state.RootStatus = authority.RootStatus(root.Status)
	state.IdentitySourceID = body.IdentitySourceID
	state.IdentitySourceVersion = body.IdentitySourceVersion
	state.GovernanceSubject = body.GovernanceSubject
	state.RootGrant = body.RootGrant
	state.TrustedNow = authorityFormatTime(now)
	return authority.VerifySignedProof(proof, request, state)
}

func (r *AuthorityRepository) lockTrustState(tx *gorm.DB, root authorityRootRow, signedProof authority.SignedProof, requireEvidence bool) (authority.TrustState, authority.Result) {
	proof := signedProof.Proof
	if requireEvidence {
		if result := lockProofEvidence(tx, root.TrustDomain, signedProof); result.Outcome != "" {
			return authority.TrustState{}, result
		}
	}
	state := authority.TrustState{}
	refs := make(map[authority.CredentialRef]struct{})
	refs[proof.Signer] = struct{}{}
	for _, hop := range proof.Delegations {
		refs[hop.Body.IssuerCredential] = struct{}{}
	}
	for _, approval := range proof.Consent.Approvals {
		refs[approval.Signer] = struct{}{}
	}
	ordered := make([]authority.CredentialRef, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].RootIncarnation != ordered[j].RootIncarnation {
			return ordered[i].RootIncarnation < ordered[j].RootIncarnation
		}
		if ordered[i].CredentialID != ordered[j].CredentialID {
			return ordered[i].CredentialID < ordered[j].CredentialID
		}
		return ordered[i].CredentialVersion < ordered[j].CredentialVersion
	})
	for _, ref := range ordered {
		credential, result := lockCredential(tx, root.TrustDomain, ref)
		if result.Outcome != "" {
			if result.Outcome == authority.OutcomeUnknown && result.Code == "PROOF_UNKNOWN" {
				continue
			}
			// Missing credentials remain absent so the domain verifier can
			// return its typed UNKNOWN/STALE outcome.
			if result.Code != "CREDENTIAL_ROW_MISSING" {
				return authority.TrustState{}, result
			}
			continue
		}
		state.Credentials = append(state.Credentials, credential)
	}
	policy, result := lockOwnerPolicy(tx, root.TrustDomain, root.RootIncarnation, proof.Consent.Body.PolicyID, proof.Consent.Body.PolicyVersion)
	if result.Outcome != "" && result.Code != "OWNER_POLICY_ROW_MISSING" {
		return authority.TrustState{}, result
	}
	if result.Outcome == "" {
		state.Policies = append(state.Policies, policy)
	}
	scopes := append([]authority.ScopedEpoch(nil), proof.Scopes...)
	sort.Slice(scopes, func(i, j int) bool {
		return authority.ScopeKeyCompare(
			authority.ScopeKey{Kind: scopes[i].Kind, ScopeID: scopes[i].ScopeID},
			authority.ScopeKey{Kind: scopes[j].Kind, ScopeID: scopes[j].ScopeID},
		) < 0
	})
	for _, scope := range scopes {
		epoch, result := lockEpoch(tx, root.TrustDomain, authority.ScopeKey{Kind: scope.Kind, ScopeID: scope.ScopeID})
		if result.Outcome != "" {
			if result.Code == "SCOPED_EPOCH_ROW_MISSING" {
				continue
			}
			return authority.TrustState{}, result
		}
		state.Epochs = append(state.Epochs, epoch)
	}
	return state, authority.Result{}
}

func lockProofEvidence(tx *gorm.DB, trustDomain string, signedProof authority.SignedProof) authority.Result {
	proof := signedProof.Proof
	type scopeKeyEvidence struct {
		ScopeKind string
		ScopeID   string
	}
	var evidenceKeys []scopeKeyEvidence
	if err := tx.Raw(`SELECT scope_kind, scope_id FROM magi_authority_proof_scope
		WHERE trust_domain = ? AND proof_id = ?`, trustDomain, signedProof.ProofID).Scan(&evidenceKeys).Error; err != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	sort.Slice(evidenceKeys, func(i, j int) bool {
		return authority.ScopeKeyCompare(
			authority.ScopeKey{Kind: authority.ScopeKind(evidenceKeys[i].ScopeKind), ScopeID: evidenceKeys[i].ScopeID},
			authority.ScopeKey{Kind: authority.ScopeKind(evidenceKeys[j].ScopeKind), ScopeID: evidenceKeys[j].ScopeID},
		) < 0
	})
	type scopeEvidence struct {
		ScopeKind   string
		ScopeID     string
		Epoch       uint64
		ScopeDigest string
		RawScope    []byte
	}
	scopes := make([]scopeEvidence, 0, len(evidenceKeys))
	for _, key := range evidenceKeys {
		var evidence scopeEvidence
		err := tx.Raw(`SELECT scope_kind, scope_id, epoch, scope_digest, raw_scope
			FROM magi_authority_proof_scope
			WHERE trust_domain = ? AND proof_id = ? AND scope_kind = ? AND scope_id = ? FOR UPDATE`,
			trustDomain, signedProof.ProofID, key.ScopeKind, key.ScopeID).
			Row().Scan(&evidence.ScopeKind, &evidence.ScopeID, &evidence.Epoch, &evidence.ScopeDigest, &evidence.RawScope)
		if err != nil {
			return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
		}
		scopes = append(scopes, evidence)
	}
	expectedScopes := append([]authority.ScopedEpoch(nil), proof.Scopes...)
	sort.Slice(expectedScopes, func(i, j int) bool {
		return authority.ScopeKeyCompare(
			authority.ScopeKey{Kind: expectedScopes[i].Kind, ScopeID: expectedScopes[i].ScopeID},
			authority.ScopeKey{Kind: expectedScopes[j].Kind, ScopeID: expectedScopes[j].ScopeID},
		) < 0
	})
	if len(scopes) != len(expectedScopes) {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "PROOF_SCOPE_EVIDENCE_MISMATCH", Detail: "scope evidence row count differs from the signed proof"}
	}
	for i, expected := range expectedScopes {
		if scopes[i].ScopeKind != string(expected.Kind) || scopes[i].ScopeID != expected.ScopeID || scopes[i].Epoch != expected.Epoch {
			return authority.Result{Outcome: authority.OutcomeConflict, Code: "PROOF_SCOPE_EVIDENCE_MISMATCH", Detail: "scope evidence differs from the signed proof"}
		}
		canonicalScope, err := canonicalStructBytes(expected)
		if err != nil {
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_SCOPE_EVIDENCE_CORRUPT", Detail: err.Error()}
		}
		if !bytes.Equal(scopes[i].RawScope, canonicalScope) ||
			scopes[i].ScopeDigest != authorityEvidenceDigest(authorityScopeEvidenceDomain, canonicalScope) {
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_SCOPE_EVIDENCE_CORRUPT", Detail: "scope evidence bytes or digest do not match the signed proof"}
		}
	}

	type delegationEvidence struct {
		HopIndex         int
		DelegationID     string
		DelegationDigest string
		RawDelegation    []byte
	}
	var delegations []delegationEvidence
	if err := tx.Raw(`SELECT hop_index, delegation_id, delegation_digest, raw_delegation
		FROM magi_authority_proof_delegation
		WHERE trust_domain = ? AND proof_id = ? ORDER BY hop_index FOR UPDATE`,
		trustDomain, signedProof.ProofID).Scan(&delegations).Error; err != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if len(delegations) != len(proof.Delegations) {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "PROOF_EVIDENCE_MISSING", Detail: "delegation evidence row count differs from the signed proof"}
	}
	for i, hop := range proof.Delegations {
		canonicalHop, err := canonicalStructBytes(hop)
		if err != nil {
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_EVIDENCE_CORRUPT", Detail: err.Error()}
		}
		if delegations[i].HopIndex != i || delegations[i].DelegationID != hop.Body.DelegationID ||
			!bytes.Equal(delegations[i].RawDelegation, canonicalHop) ||
			delegations[i].DelegationDigest != authorityEvidenceDigest(authorityDelegationEvidenceDomain, canonicalHop) {
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_EVIDENCE_CORRUPT", Detail: "delegation evidence does not match the signed proof"}
		}
	}

	type consentEvidence struct {
		ConsentID     string
		PolicyID      string
		PolicyVersion string
		ConsentDigest string
		RawConsent    []byte
	}
	var consent consentEvidence
	err := tx.Raw(`SELECT consent_id, policy_id, policy_version, consent_digest, raw_consent
		FROM magi_authority_proof_consent WHERE trust_domain = ? AND proof_id = ? FOR UPDATE`,
		trustDomain, signedProof.ProofID).
		Row().Scan(&consent.ConsentID, &consent.PolicyID, &consent.PolicyVersion, &consent.ConsentDigest, &consent.RawConsent)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "PROOF_EVIDENCE_MISSING", Detail: "consent evidence is missing"}
	}
	if err != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	canonicalConsent, err := canonicalStructBytes(proof.Consent)
	if err != nil {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_EVIDENCE_CORRUPT", Detail: err.Error()}
	}
	if consent.ConsentID != proof.Consent.Body.ConsentID ||
		consent.PolicyID != proof.Consent.Body.PolicyID ||
		consent.PolicyVersion != proof.Consent.Body.PolicyVersion ||
		!bytes.Equal(consent.RawConsent, canonicalConsent) ||
		consent.ConsentDigest != authorityEvidenceDigest(authorityConsentEvidenceDomain, canonicalConsent) {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_EVIDENCE_CORRUPT", Detail: "consent evidence does not match the signed proof"}
	}
	return authority.Result{}
}

func lockCredential(tx *gorm.DB, trustDomain string, ref authority.CredentialRef) (authority.Credential, authority.Result) {
	var row authorityCredentialRow
	err := tx.Raw(`SELECT trust_domain, root_incarnation, credential_id, credential_version,
			subject_id, identity_source_id, identity_source_version, algorithm,
			public_key, not_before, expires_at, status, credential_scope_id, raw_credential
		FROM magi_authority_credential
		WHERE trust_domain = ? AND root_incarnation = ? AND credential_id = ? AND credential_version = ?
		FOR UPDATE`, trustDomain, ref.RootIncarnation, ref.CredentialID, ref.CredentialVersion).
		Row().Scan(&row.TrustDomain, &row.RootIncarnation, &row.CredentialID, &row.CredentialVersion,
		&row.SubjectID, &row.IdentitySourceID, &row.IdentitySourceVersion, &row.Algorithm,
		&row.PublicKey, &row.NotBefore, &row.ExpiresAt, &row.Status, &row.CredentialScopeID, &row.RawCredential)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "CREDENTIAL_ROW_MISSING"}
	}
	if err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if err := requireCanonicalStored(row.RawCredential); err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "AUTHORITY_STORAGE_CORRUPT", Detail: err.Error()}
	}
	var credential authority.Credential
	if err := json.Unmarshal(row.RawCredential, &credential); err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "AUTHORITY_STORAGE_CORRUPT", Detail: err.Error()}
	}
	if credential.Ref != ref || credential.Subject.SubjectID != row.SubjectID ||
		credential.Subject.IdentitySourceID != row.IdentitySourceID ||
		credential.Subject.IdentitySourceVersion != row.IdentitySourceVersion ||
		credential.Algorithm != row.Algorithm ||
		credential.PublicKey != base64.StdEncoding.EncodeToString(row.PublicKey) ||
		credential.Scope.Kind != authority.ScopeCredential || credential.Scope.ScopeID != row.CredentialScopeID ||
		credential.NotBefore != authorityFormatTime(row.NotBefore) || credential.ExpiresAt != authorityFormatTime(row.ExpiresAt) {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_STORAGE_CONFLICT", Detail: "credential columns differ from canonical raw evidence"}
	}
	// Status and retirement time are mutable authority state; the signed
	// credential body records the identity/key/validity that were bootstrapped.
	credential.Status = authority.CredentialStatus(row.Status)
	return credential, authority.Result{}
}

func lockOwnerPolicy(tx *gorm.DB, trustDomain, rootIncarnation, policyID, policyVersion string) (authority.OwnerPolicy, authority.Result) {
	var row authorityPolicyRow
	err := tx.Raw(`SELECT trust_domain, root_incarnation, policy_id, policy_version, status, policy_digest, raw_policy
		FROM magi_authority_owner_policy
		WHERE trust_domain = ? AND root_incarnation = ? AND policy_id = ? AND policy_version = ? FOR UPDATE`,
		trustDomain, rootIncarnation, policyID, policyVersion).
		Row().Scan(&row.TrustDomain, &row.RootIncarnation, &row.PolicyID, &row.PolicyVersion, &row.Status, &row.PolicyDigest, &row.RawPolicy)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "OWNER_POLICY_ROW_MISSING"}
	}
	if err != nil {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if err := requireCanonicalStored(row.RawPolicy); err != nil {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "AUTHORITY_STORAGE_CORRUPT", Detail: err.Error()}
	}
	if authorityEvidenceDigest(authorityOwnerPolicyEvidenceDomain, row.RawPolicy) != row.PolicyDigest {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_STORAGE_CONFLICT", Detail: "owner policy digest mismatch"}
	}
	var policy authority.OwnerPolicy
	if err := json.Unmarshal(row.RawPolicy, &policy); err != nil {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "AUTHORITY_STORAGE_CORRUPT", Detail: err.Error()}
	}
	if policy.RootIncarnation != row.RootIncarnation || policy.PolicyID != row.PolicyID || policy.PolicyVersion != row.PolicyVersion {
		return authority.OwnerPolicy{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_STORAGE_CONFLICT", Detail: "owner policy columns differ from canonical raw evidence"}
	}
	policy.Status = authority.PolicyStatus(row.Status)
	return policy, authority.Result{}
}

func lockEpoch(tx *gorm.DB, trustDomain string, scope authority.ScopeKey) (authority.EpochState, authority.Result) {
	var row authorityEpochRow
	err := tx.Raw(`SELECT scope_kind, scope_id, epoch, revoked, row_version
		FROM magi_authority_epoch
		WHERE trust_domain = ? AND scope_kind = ? AND scope_id = ? FOR UPDATE`,
		trustDomain, scope.Kind, scope.ScopeID).
		Row().Scan(&row.ScopeKind, &row.ScopeID, &row.Epoch, &row.Revoked, &row.RowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "SCOPED_EPOCH_ROW_MISSING"}
	}
	if err != nil {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return authority.EpochState{
		Scope: authority.ScopeKey{Kind: authority.ScopeKind(row.ScopeKind), ScopeID: row.ScopeID},
		Epoch: row.Epoch, Revoked: row.Revoked, RowVersion: row.RowVersion,
	}, authority.Result{}
}

func (r *AuthorityRepository) RegisterProof(ctx context.Context, raw []byte, expectedID string) authority.Result {
	if result := r.capabilityResult(); result != nil {
		return *result
	}
	proof, parseResult := authority.ParseSignedProof(raw, expectedID)
	if parseResult.Outcome != authority.OutcomeValid {
		return parseResult
	}
	if result := authority.ValidateProofStructureLimits(proof); result.Outcome != "" {
		return result
	}
	tx := r.begin(ctx)
	if tx.Error != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: tx.Error.Error()}
	}
	defer func() { _ = tx.Rollback().Error }()
	trustDomain := proof.Proof.TrustDomain
	root, result := lockActiveAuthorityRoot(tx, trustDomain)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return result
	}
	if result := authorityRootStatusResult(root.Status); result.Outcome != "" {
		_ = tx.Rollback().Error
		return result
	}
	existing, result := lockAuthorityProof(tx, trustDomain, expectedID)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return result
	}
	digest, _, err := authority.ProofDigest(proof.Proof)
	if err != nil {
		_ = tx.Rollback().Error
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_DIGEST_INVALID", Detail: err.Error()}
	}
	if existing != nil {
		if !bytes.Equal(existing.RawProof, raw) || existing.ProofDigest != digest {
			_ = tx.Rollback().Error
			return authority.Result{Outcome: authority.OutcomeConflict, Code: "PROOF_ID_REUSE_CONFLICT"}
		}
		request := verificationRequestFromProof(proof)
		result := r.verifyProofInTx(tx, request)
		if result.Outcome != authority.OutcomeValid {
			_ = tx.Rollback().Error
			return result
		}
		if err := tx.Commit().Error; err != nil {
			return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
		}
		result.Code = "PROOF_ALREADY_REGISTERED"
		return result
	}
	state, result := r.lockTrustState(tx, root, proof, false)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return result
	}
	now, result := authorityDBTime(tx)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return result
	}
	body, fingerprint, rootResult := authority.ReadBootstrap(root.RawBootstrap, root.RootFingerprint, r.trustWithTime(now))
	if rootResult.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return rootResult
	}
	state.TrustDomain = body.TrustDomain
	state.RootIncarnation = body.RootIncarnation
	state.RootFingerprint = fingerprint
	state.RootStatus = authority.RootStatus(root.Status)
	state.IdentitySourceID = body.IdentitySourceID
	state.IdentitySourceVersion = body.IdentitySourceVersion
	state.GovernanceSubject = body.GovernanceSubject
	state.RootGrant = body.RootGrant
	state.TrustedNow = authorityFormatTime(now)
	verifyRequest := verificationRequestFromProof(proof)
	verifyResult := authority.VerifySignedProof(proof, verifyRequest, state)
	if verifyResult.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return verifyResult
	}
	if err := tx.Exec(`INSERT INTO magi_authority_proof
			(trust_domain, proof_id, proof_version, proof_digest, root_incarnation, root_fingerprint,
			 subject_id, identity_source_id, identity_source_version, signer_credential_id,
			 signer_credential_version, resource, action, tenant_id, operation, case_id, case_round,
			 execution_generation, issued_at, expires_at, registered_at, raw_proof)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trustDomain, proof.ProofID, proof.Proof.Version, digest, proof.Proof.RootIncarnation, fingerprint,
		proof.Proof.Subject.SubjectID, proof.Proof.Subject.IdentitySourceID, proof.Proof.Subject.IdentitySourceVersion,
		proof.Proof.Signer.CredentialID, proof.Proof.Signer.CredentialVersion,
		proof.Proof.Resource, proof.Proof.Action, proof.Proof.Tenant, proof.Proof.Operation,
		proof.Proof.CaseID, proof.Proof.Round, proof.Proof.ExecutionGeneration,
		mustAuthorityTime(proof.Proof.IssuedAt), mustAuthorityTime(proof.Proof.ExpiresAt),
		now, raw).Error; err != nil {
		_ = tx.Rollback().Error
		return authorityDuplicateOrStoreError(err, "PROOF_ID_REUSE_CONFLICT")
	}
	for _, scope := range proof.Proof.Scopes {
		scopeRaw, err := canonicalStructBytes(scope)
		if err != nil {
			_ = tx.Rollback().Error
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "PROOF_SCOPE_EVIDENCE_CORRUPT", Detail: err.Error()}
		}
		if err := tx.Exec(`INSERT INTO magi_authority_proof_scope
				(trust_domain, proof_id, scope_kind, scope_id, epoch, scope_digest, raw_scope)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
			trustDomain, proof.ProofID, scope.Kind, scope.ScopeID, scope.Epoch,
			authorityEvidenceDigest(authorityScopeEvidenceDomain, scopeRaw), scopeRaw).Error; err != nil {
			_ = tx.Rollback().Error
			return authorityDuplicateOrStoreError(err, "PROOF_SCOPE_EVIDENCE_CONFLICT")
		}
	}
	for i, hop := range proof.Proof.Delegations {
		hopRaw, err := canonicalStructBytes(hop)
		if err != nil {
			_ = tx.Rollback().Error
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "DELEGATION_INVALID", Detail: err.Error()}
		}
		if err := tx.Exec(`INSERT INTO magi_authority_proof_delegation
				(trust_domain, proof_id, hop_index, delegation_id, delegation_digest,
				 issuer_subject_id, delegate_subject_id, raw_delegation)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			trustDomain, proof.ProofID, i, hop.Body.DelegationID,
			authorityEvidenceDigest(authorityDelegationEvidenceDomain, hopRaw),
			hop.Body.Issuer.SubjectID, hop.Body.Delegate.SubjectID, hopRaw).Error; err != nil {
			_ = tx.Rollback().Error
			return authorityDuplicateOrStoreError(err, "DELEGATION_EVIDENCE_CONFLICT")
		}
	}
	consentRaw, err := canonicalStructBytes(proof.Proof.Consent)
	if err != nil {
		_ = tx.Rollback().Error
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "CONSENT_BINDING_MISMATCH", Detail: err.Error()}
	}
	policy := state.Policies[0]
	if err := tx.Exec(`INSERT INTO magi_authority_proof_consent
			(trust_domain, proof_id, consent_id, policy_id, policy_version, consent_digest,
			 threshold, approval_count, execution_generation, raw_consent)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trustDomain, proof.ProofID, proof.Proof.Consent.Body.ConsentID, policy.PolicyID, policy.PolicyVersion,
		authorityEvidenceDigest(authorityConsentEvidenceDomain, consentRaw),
		policy.Threshold, len(proof.Proof.Consent.Approvals),
		proof.Proof.Consent.Body.ExecutionGeneration, consentRaw).Error; err != nil {
		_ = tx.Rollback().Error
		return authorityDuplicateOrStoreError(err, "CONSENT_EVIDENCE_CONFLICT")
	}
	if err := tx.Commit().Error; err != nil {
		return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
	}
	verifyResult.Code = "PROOF_REGISTERED"
	return verifyResult
}

func verificationRequestFromProof(proof authority.SignedProof) authority.VerificationRequest {
	return authority.VerificationRequest{
		TrustDomain: proof.Proof.TrustDomain, ProofID: proof.ProofID,
		Subject: proof.Proof.Subject, Resource: proof.Proof.Resource, Action: proof.Proof.Action,
		Tenant: proof.Proof.Tenant, Operation: proof.Proof.Operation, CaseID: proof.Proof.CaseID,
		Round: proof.Proof.Round, ExecutionGeneration: proof.Proof.ExecutionGeneration,
		Scopes: append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...),
	}
}

func canonicalStructBytes(v any) ([]byte, error) {
	c, err := canonical.CanonicalizeStruct(v)
	if err != nil {
		return nil, err
	}
	return c.Bytes(), nil
}

func requireCanonicalStored(raw []byte) error {
	c, err := canonical.CanonicalizeWithLimits(raw, canonical.Limits{
		MaxBytes: 4 << 20, MaxDepth: 32, MaxMembers: 131072, MaxStringBytes: 16384,
	})
	if err != nil {
		return err
	}
	if !bytes.Equal(c.Bytes(), raw) {
		return fmt.Errorf("stored bytes are not canonical")
	}
	return nil
}

func authorityEvidenceDigest(domain string, raw []byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func mustAuthorityTime(value string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000Z", value)
	return t
}

func authorityDuplicateOrStoreError(err error, code string) authority.Result {
	if err == nil {
		return authority.Result{}
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "duplicate") || strings.Contains(text, "1062") {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: code, Detail: err.Error()}
	}
	return authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
}

const (
	authorityCommandRevocation = "REVOCATION"
	authorityCommandRotation   = "CREDENTIAL_ROTATION"
)

func lockAuthorityCommand(tx *gorm.DB, trustDomain, kind, commandID string) (*authorityCommandRow, authority.Result) {
	var row authorityCommandRow
	err := tx.Raw(`SELECT command_kind, command_digest, raw_command,
			result_scope_kind, result_scope_id, result_epoch, result_row_version, result_revoked,
			result_credential_id, result_credential_version, result_credential_raw
		FROM magi_authority_command
		WHERE trust_domain = ? AND command_id = ? FOR UPDATE`,
		trustDomain, commandID).
		Row().Scan(&row.CommandKind, &row.CommandDigest, &row.RawCommand,
		&row.ResultScopeKind, &row.ResultScopeID, &row.ResultEpoch, &row.ResultRowVersion, &row.ResultRevoked,
		&row.ResultCredentialID, &row.ResultCredentialVersion, &row.ResultCredentialRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if row.CommandKind != kind {
		return nil, authority.Result{Outcome: authority.OutcomeConflict, Code: "COMMAND_ID_CONFLICT"}
	}
	return &row, authority.Result{}
}

func commandEvidence(command any) ([]byte, string, authority.Result) {
	raw, err := canonicalStructBytes(command)
	if err != nil {
		return nil, "", authority.Result{Outcome: authority.OutcomeDenied, Code: "COMMAND_INVALID", Detail: err.Error()}
	}
	return raw, authorityEvidenceDigest(authorityCommandEvidenceDomain, raw), authority.Result{}
}

func replayRevocationCommand(row authorityCommandRow, raw []byte) (authority.EpochState, authority.Result) {
	if row.CommandKind != authorityCommandRevocation ||
		row.CommandDigest != authorityEvidenceDigest(authorityCommandEvidenceDomain, row.RawCommand) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "COMMAND_RESULT_CORRUPT"}
	}
	if !bytes.Equal(row.RawCommand, raw) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "COMMAND_ID_CONFLICT"}
	}
	if row.ResultScopeKind != string(authority.ScopeContext) &&
		row.ResultScopeKind != string(authority.ScopeAuthority) &&
		row.ResultScopeKind != string(authority.ScopeCredential) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "COMMAND_RESULT_CORRUPT"}
	}
	return authority.EpochState{
		Scope: authority.ScopeKey{Kind: authority.ScopeKind(row.ResultScopeKind), ScopeID: row.ResultScopeID},
		Epoch: row.ResultEpoch, Revoked: row.ResultRevoked, RowVersion: row.ResultRowVersion,
	}, authority.Result{Outcome: authority.OutcomeValid, Code: "REVOCATION_COMMAND_REPLAY"}
}

func replayCredentialRotationCommand(row authorityCommandRow, raw []byte) (authority.Credential, authority.Result) {
	if row.CommandKind != authorityCommandRotation ||
		row.CommandDigest != authorityEvidenceDigest(authorityCommandEvidenceDomain, row.RawCommand) {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "COMMAND_RESULT_CORRUPT"}
	}
	if !bytes.Equal(row.RawCommand, raw) {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "COMMAND_ID_CONFLICT"}
	}
	if len(row.ResultCredentialRaw) == 0 {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "COMMAND_RESULT_CORRUPT"}
	}
	if err := requireCanonicalStored(row.ResultCredentialRaw); err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "COMMAND_RESULT_CORRUPT", Detail: err.Error()}
	}
	var credential authority.Credential
	if err := json.Unmarshal(row.ResultCredentialRaw, &credential); err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "COMMAND_RESULT_CORRUPT", Detail: err.Error()}
	}
	if credential.Ref.CredentialID != row.ResultCredentialID || credential.Ref.CredentialVersion != row.ResultCredentialVersion {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "COMMAND_RESULT_CORRUPT"}
	}
	return credential, authority.Result{Outcome: authority.OutcomeValid, Code: "CREDENTIAL_ROTATION_COMMAND_REPLAY"}
}

func insertAuthorityCommand(tx *gorm.DB, trustDomain, kind, commandID string, commandRaw []byte, commandDigest string,
	epoch *authority.EpochState, credential *authority.Credential, completedAt time.Time) authority.Result {
	var scopeKind, scopeID, credentialID string
	var epochValue, rowVersion, credentialVersion uint64
	var revoked bool
	var credentialRaw []byte
	if epoch != nil {
		scopeKind, scopeID = string(epoch.Scope.Kind), epoch.Scope.ScopeID
		epochValue, rowVersion, revoked = epoch.Epoch, epoch.RowVersion, epoch.Revoked
	}
	if credential != nil {
		credentialID, credentialVersion = credential.Ref.CredentialID, credential.Ref.CredentialVersion
		var err error
		credentialRaw, err = canonicalStructBytes(credential)
		if err != nil {
			return authority.Result{Outcome: authority.OutcomeDenied, Code: "COMMAND_RESULT_CORRUPT", Detail: err.Error()}
		}
	}
	if err := tx.Exec(`INSERT INTO magi_authority_command
			(trust_domain, command_kind, command_id, command_digest, raw_command,
			 result_scope_kind, result_scope_id, result_epoch, result_row_version, result_revoked,
			 result_credential_id, result_credential_version, result_credential_raw, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trustDomain, kind, commandID, commandDigest, commandRaw,
		scopeKind, scopeID, epochValue, rowVersion, revoked,
		credentialID, credentialVersion, credentialRaw, completedAt).Error; err != nil {
		return authorityDuplicateOrStoreError(err, "COMMAND_ID_CONFLICT")
	}
	return authority.Result{}
}

func (r *AuthorityRepository) ReadEpoch(ctx context.Context, trustDomain string, scope authority.ScopeKey) (authority.EpochState, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return authority.EpochState{}, *result
	}
	if strings.TrimSpace(trustDomain) == "" || !validAuthorityScope(scope) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "SCOPE_INVALID"}
	}
	var row authorityEpochRow
	err := r.db.WithContext(ctx).Raw(`SELECT scope_kind, scope_id, epoch, revoked, row_version
		FROM magi_authority_epoch WHERE trust_domain = ? AND scope_kind = ? AND scope_id = ?`,
		trustDomain, scope.Kind, scope.ScopeID).
		Row().Scan(&row.ScopeKind, &row.ScopeID, &row.Epoch, &row.Revoked, &row.RowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "SCOPED_EPOCH_UNKNOWN"}
	}
	if err != nil {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return authority.EpochState{
		Scope: authority.ScopeKey{Kind: authority.ScopeKind(row.ScopeKind), ScopeID: row.ScopeID},
		Epoch: row.Epoch, Revoked: row.Revoked, RowVersion: row.RowVersion,
	}, authority.Result{Outcome: authority.OutcomeValid, Code: "SCOPED_EPOCH_READBACK_VALID"}
}

// RevokeScope authenticates a governance command and advances exactly one
// scoped epoch durably. Expected epoch and row version make stale/A-B-A commands
// fail closed. No production caller is wired while C2 is disabled.
func (r *AuthorityRepository) RevokeScope(ctx context.Context, command authority.SignedRevocationCommand) (authority.EpochState, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return authority.EpochState{}, *result
	}
	body := command.Body
	if strings.TrimSpace(body.TrustDomain) == "" || !validAuthorityScope(body.Scope) || body.ExpectedRowVersion == 0 {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "SCOPE_INVALID"}
	}
	if strings.TrimSpace(body.CommandID) == "" {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "REVOCATION_COMMAND_INVALID"}
	}
	tx := r.begin(ctx)
	if tx.Error != nil {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: tx.Error.Error()}
	}
	defer func() { _ = tx.Rollback().Error }()
	root, result := lockActiveAuthorityRoot(tx, body.TrustDomain)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, result
	}
	if result := authorityRootStatusResult(root.Status); result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, result
	}
	commandRaw, commandDigest, commandResult := commandEvidence(command)
	if commandResult.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, commandResult
	}
	if existing, lookupResult := lockAuthorityCommand(tx, body.TrustDomain, authorityCommandRevocation, body.CommandID); lookupResult.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, lookupResult
	} else if existing != nil {
		replayState, replayResult := replayRevocationCommand(*existing, commandRaw)
		if replayResult.Outcome != authority.OutcomeValid {
			_ = tx.Rollback().Error
			return replayState, replayResult
		}
		if err := tx.Commit().Error; err != nil {
			return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
		}
		return replayState, replayResult
	}
	current, result := lockEpoch(tx, body.TrustDomain, body.Scope)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		if result.Code == "SCOPED_EPOCH_ROW_MISSING" {
			result = authority.Result{Outcome: authority.OutcomeUnknown, Code: "SCOPED_EPOCH_UNKNOWN"}
		}
		return authority.EpochState{}, result
	}
	state, now, result := r.lockCommandState(tx, root, []authority.CredentialRef{command.Signer})
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, result
	}
	if result := authority.VerifyRevocationCommand(command, state); result.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return authority.EpochState{}, result
	}
	if current.Epoch != body.ExpectedEpoch || current.RowVersion != body.ExpectedRowVersion || current.Epoch == ^uint64(0) {
		_ = tx.Rollback().Error
		return current, authority.Result{Outcome: authority.OutcomeStale, Code: "EPOCH_CAS_STALE"}
	}
	if current.Revoked {
		_ = tx.Rollback().Error
		return current, authority.Result{Outcome: authority.OutcomeConflict, Code: "SCOPE_ALREADY_REVOKED"}
	}
	next := current
	next.Epoch++
	next.Revoked = true
	next.RowVersion++
	updated := tx.Exec(`UPDATE magi_authority_epoch
		SET epoch = ?, revoked = 1, row_version = row_version + 1,
		    reason = ?, updated_at = ?
		WHERE trust_domain = ? AND scope_kind = ? AND scope_id = ?
		  AND epoch = ? AND row_version = ? AND revoked = 0`,
		next.Epoch, body.Reason, now, body.TrustDomain, body.Scope.Kind, body.Scope.ScopeID,
		current.Epoch, current.RowVersion)
	if updated.Error != nil {
		_ = tx.Rollback().Error
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: updated.Error.Error()}
	}
	if updated.RowsAffected != 1 {
		_ = tx.Rollback().Error
		return current, authority.Result{Outcome: authority.OutcomeStale, Code: "EPOCH_CAS_STALE"}
	}
	if result := insertAuthorityCommand(tx, body.TrustDomain, authorityCommandRevocation,
		body.CommandID, commandRaw, commandDigest, &next, nil, now); result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.EpochState{}, result
	}
	if err := tx.Commit().Error; err != nil {
		return authority.EpochState{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
	}
	return next, authority.Result{Outcome: authority.OutcomeValid, Code: "SCOPE_REVOKED"}
}

// RotateCredential authenticates and applies an exact next-version credential
// replacement. The old credential retires and its CREDENTIAL scope epoch
// advances under the same active-root lock transaction.
func (r *AuthorityRepository) RotateCredential(ctx context.Context, command authority.SignedCredentialRotationCommand) (authority.Credential, authority.Result) {
	if result := r.capabilityResult(); result != nil {
		return authority.Credential{}, *result
	}
	body := command.Body
	if strings.TrimSpace(body.TrustDomain) == "" || body.ExpectedRowVersion == 0 {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "CREDENTIAL_ROTATION_INVALID"}
	}
	if strings.TrimSpace(body.CommandID) == "" {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "CREDENTIAL_ROTATION_INVALID"}
	}
	tx := r.begin(ctx)
	if tx.Error != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: tx.Error.Error()}
	}
	defer func() { _ = tx.Rollback().Error }()
	root, result := lockActiveAuthorityRoot(tx, body.TrustDomain)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	if result := authorityRootStatusResult(root.Status); result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	commandRaw, commandDigest, commandResult := commandEvidence(command)
	if commandResult.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, commandResult
	}
	if existing, lookupResult := lockAuthorityCommand(tx, body.TrustDomain, authorityCommandRotation, body.CommandID); lookupResult.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, lookupResult
	} else if existing != nil {
		replayCredential, replayResult := replayCredentialRotationCommand(*existing, commandRaw)
		if replayResult.Outcome != authority.OutcomeValid {
			_ = tx.Rollback().Error
			return replayCredential, replayResult
		}
		if err := tx.Commit().Error; err != nil {
			return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
		}
		return replayCredential, replayResult
	}
	if body.NewCredential.Scope.Kind != authority.ScopeCredential || !validAuthorityScope(body.NewCredential.Scope) {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "CREDENTIAL_ROTATION_INVALID"}
	}
	epochScope := body.NewCredential.Scope
	current, result := lockEpoch(tx, body.TrustDomain, epochScope)
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	state, now, result := r.lockCommandState(tx, root, []authority.CredentialRef{command.Signer, body.OldCredential})
	if result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	if result := authority.VerifyCredentialRotationCommand(command, state); result.Outcome != authority.OutcomeValid {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	if current.Revoked {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeRevoked, Code: "CREDENTIAL_SCOPE_REVOKED"}
	}
	if current.Epoch != body.ExpectedEpoch || current.RowVersion != body.ExpectedRowVersion || current.Epoch == ^uint64(0) {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeStale, Code: "CREDENTIAL_ROTATION_STALE"}
	}
	rawCredential, err := canonicalStructBytes(body.NewCredential)
	if err != nil {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "CREDENTIAL_ROTATION_INVALID", Detail: err.Error()}
	}
	publicKey, err := base64.StdEncoding.DecodeString(body.NewCredential.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeDenied, Code: "CREDENTIAL_ROTATION_INVALID", Detail: "new public key is not Ed25519"}
	}
	retired := tx.Exec(`UPDATE magi_authority_credential
		SET status = 'RETIRED', retired_at = ?
		WHERE trust_domain = ? AND root_incarnation = ? AND credential_id = ? AND credential_version = ?
		  AND status = 'ACTIVE'`,
		now, body.TrustDomain, body.OldCredential.RootIncarnation,
		body.OldCredential.CredentialID, body.OldCredential.CredentialVersion)
	if retired.Error != nil {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: retired.Error.Error()}
	}
	if retired.RowsAffected != 1 {
		_ = tx.Rollback().Error
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeStale, Code: "CREDENTIAL_ROTATION_STALE"}
	}
	if err := tx.Exec(`INSERT INTO magi_authority_credential
			(trust_domain, root_incarnation, credential_id, credential_version,
			 subject_id, identity_source_id, identity_source_version, algorithm,
			 public_key, not_before, expires_at, status, credential_scope_id,
			 created_at, raw_credential)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		body.TrustDomain, body.NewCredential.Ref.RootIncarnation, body.NewCredential.Ref.CredentialID,
		body.NewCredential.Ref.CredentialVersion, body.NewCredential.Subject.SubjectID,
		body.NewCredential.Subject.IdentitySourceID, body.NewCredential.Subject.IdentitySourceVersion,
		body.NewCredential.Algorithm, publicKey, mustAuthorityTime(body.NewCredential.NotBefore),
		mustAuthorityTime(body.NewCredential.ExpiresAt), body.NewCredential.Status,
		body.NewCredential.Scope.ScopeID, now, rawCredential).Error; err != nil {
		_ = tx.Rollback().Error
		return authority.Credential{}, authorityDuplicateOrStoreError(err, "CREDENTIAL_VERSION_CONFLICT")
	}
	nextEpoch := current
	nextEpoch.Epoch++
	nextEpoch.RowVersion++
	updatedEpoch := tx.Exec(`UPDATE magi_authority_epoch
		SET epoch = ?, row_version = row_version + 1, reason = ?, updated_at = ?
		WHERE trust_domain = ? AND scope_kind = 'CREDENTIAL' AND scope_id = ?
		  AND epoch = ? AND row_version = ? AND revoked = 0`,
		nextEpoch.Epoch, body.Reason, now, body.TrustDomain, epochScope.ScopeID,
		current.Epoch, current.RowVersion)
	if updatedEpoch.Error != nil || updatedEpoch.RowsAffected != 1 {
		_ = tx.Rollback().Error
		if updatedEpoch.Error != nil {
			return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: updatedEpoch.Error.Error()}
		}
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeStale, Code: "CREDENTIAL_ROTATION_STALE"}
	}
	if result := insertAuthorityCommand(tx, body.TrustDomain, authorityCommandRotation,
		body.CommandID, commandRaw, commandDigest, &nextEpoch, &body.NewCredential, now); result.Outcome != "" {
		_ = tx.Rollback().Error
		return authority.Credential{}, result
	}
	if err := tx.Commit().Error; err != nil {
		return authority.Credential{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_COMMIT_UNKNOWN", Detail: err.Error()}
	}
	return body.NewCredential, authority.Result{Outcome: authority.OutcomeValid, Code: "CREDENTIAL_ROTATED"}
}

func (r *AuthorityRepository) lockCommandState(tx *gorm.DB, root authorityRootRow, refs []authority.CredentialRef) (authority.TrustState, time.Time, authority.Result) {
	if result := authorityRootStatusResult(root.Status); result.Outcome != "" {
		return authority.TrustState{}, time.Time{}, result
	}
	unique := make(map[authority.CredentialRef]struct{}, len(refs))
	for _, ref := range refs {
		unique[ref] = struct{}{}
	}
	ordered := make([]authority.CredentialRef, 0, len(unique))
	for ref := range unique {
		ordered = append(ordered, ref)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].RootIncarnation != ordered[j].RootIncarnation {
			return ordered[i].RootIncarnation < ordered[j].RootIncarnation
		}
		if ordered[i].CredentialID != ordered[j].CredentialID {
			return ordered[i].CredentialID < ordered[j].CredentialID
		}
		return ordered[i].CredentialVersion < ordered[j].CredentialVersion
	})
	state := authority.TrustState{}
	for _, ref := range ordered {
		credential, result := lockCredential(tx, root.TrustDomain, ref)
		if result.Outcome != "" {
			if result.Code == "CREDENTIAL_ROW_MISSING" {
				continue
			}
			return authority.TrustState{}, time.Time{}, result
		}
		state.Credentials = append(state.Credentials, credential)
	}
	// Trusted time is read only after every credential/epoch row is locked.
	now, result := authorityDBTime(tx)
	if result.Outcome != "" {
		return authority.TrustState{}, time.Time{}, result
	}
	body, fingerprint, rootResult := authority.ReadBootstrap(root.RawBootstrap, root.RootFingerprint, r.trustWithTime(now))
	if rootResult.Outcome != authority.OutcomeValid {
		return authority.TrustState{}, time.Time{}, rootResult
	}
	state.TrustDomain = body.TrustDomain
	state.RootIncarnation = body.RootIncarnation
	state.RootFingerprint = fingerprint
	state.RootStatus = authority.RootStatus(root.Status)
	state.IdentitySourceID = body.IdentitySourceID
	state.IdentitySourceVersion = body.IdentitySourceVersion
	state.GovernanceSubject = body.GovernanceSubject
	state.RootGrant = body.RootGrant
	state.TrustedNow = authorityFormatTime(now)
	return state, now, authority.Result{}
}

func validAuthorityScope(scope authority.ScopeKey) bool {
	if strings.TrimSpace(scope.ScopeID) == "" {
		return false
	}
	switch scope.Kind {
	case authority.ScopeAuthority, authority.ScopeContext, authority.ScopeCredential:
		return true
	default:
		return false
	}
}
