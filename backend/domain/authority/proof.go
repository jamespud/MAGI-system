package authority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/canonical"
)

const (
	proofSignatureDomain    = "magi:authority-proof-signature:v1\n"
	consentEvidenceDomain   = "magi:authority-consent-evidence:v1\n"
	consentApprovalDomain   = "magi:authority-consent-approval:v1\n"
	timeLayoutUTC           = "2006-01-02T15:04:05.000Z"
	maxProofBytes           = 2 << 20
	maxProofMembers         = 131072
	maxProofStringBytes     = 16384
	proofReadbackRequiresID = "PROOF_ID_REQUIRED"
)

var proofLimits = canonical.Limits{
	MaxBytes:       maxProofBytes,
	MaxDepth:       32,
	MaxMembers:     maxProofMembers,
	MaxStringBytes: maxProofStringBytes,
}

func validResult() Result {
	return Result{
		Outcome:                 OutcomeValid,
		Code:                    "LOCAL_AUTH_VALID",
		LocalAuthenticationOnly: true,
		CommitEligible:          false,
		MutationAuthorized:      false,
	}
}

func failure(outcome Outcome, code, detail string) Result {
	return Result{Outcome: outcome, Code: code, Detail: detail}
}

func canonicalStruct(v any) (canonical.Canonical, error) {
	return canonical.CanonicalizeStructWithLimits(v, proofLimits)
}

// ProofDigest returns the existing C0 authority_proof digest over canonical
// proof-body bytes. This is intentionally the already-registered C0 artifact
// type, so the proof id remains the standard aprf_<digest> identity.
func ProofDigest(body Proof) (string, []byte, error) {
	c, err := canonicalStruct(body)
	if err != nil {
		return "", nil, err
	}
	digest, err := c.Digest(canonical.TypeAuthorityProof)
	if err != nil {
		return "", nil, err
	}
	return digest, c.Bytes(), nil
}

func proofIDForDigest(digest string) string { return "aprf_" + digest }

// ProofSigningBytes returns the explicitly domain-separated bytes covered by
// the final proof signature. The signature envelope itself is not included.
func ProofSigningBytes(body Proof) ([]byte, error) {
	_, raw, err := ProofDigest(body)
	if err != nil {
		return nil, err
	}
	msg := make([]byte, 0, len(proofSignatureDomain)+len(raw))
	msg = append(msg, proofSignatureDomain...)
	msg = append(msg, raw...)
	return msg, nil
}

// ParseSignedProof requires exact canonical raw bytes and verifies the C0
// authority_proof identity. It does not grant authority; callers must still
// invoke VerifySignedProof against live state.
func ParseSignedProof(raw []byte, expectedID string) (SignedProof, Result) {
	if strings.TrimSpace(expectedID) == "" {
		return SignedProof{}, failure(OutcomeDenied, proofReadbackRequiresID, "readback requires an expected proof identity")
	}
	c, err := canonical.CanonicalizeWithLimits(raw, proofLimits)
	if err != nil {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_BYTES_INVALID", err.Error())
	}
	if !bytes.Equal(c.Bytes(), raw) {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_BYTES_NON_CANONICAL", "stored proof bytes must already be canonical")
	}
	var proof SignedProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_SHAPE_INVALID", err.Error())
	}
	reencoded, err := canonicalStruct(proof)
	if err != nil || !bytes.Equal(reencoded.Bytes(), raw) {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_SHAPE_INVALID", "proof contains missing or unknown fields")
	}
	if proof.Proof.Version != ProofVersion {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_VERSION_UNSUPPORTED", fmt.Sprintf("got %q", proof.Proof.Version))
	}
	digest, _, err := ProofDigest(proof.Proof)
	if err != nil {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_DIGEST_INVALID", err.Error())
	}
	wantID := proofIDForDigest(digest)
	if proof.ProofID != expectedID || proof.ProofID != wantID {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_IDENTITY_MISMATCH", "proof id is not the exact C0 aprf_ identity")
	}
	sig, err := base64.StdEncoding.DecodeString(proof.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return SignedProof{}, failure(OutcomeDenied, "PROOF_SIGNATURE_INVALID", "signature is not a base64 Ed25519 signature")
	}
	return proof, Result{Outcome: OutcomeValid, Code: "PROOF_BYTES_VALID"}
}

// VerifySignedProof performs online local authentication against a complete
// live-state snapshot. It is a pure function; persistence must gather the state
// under the C2 transaction/lock order before calling it.
func VerifySignedProof(proof SignedProof, request VerificationRequest, state TrustState) Result {
	body := proof.Proof
	if err := validateRequest(request); err != nil {
		return failure(OutcomeDenied, "REQUEST_INVALID", err.Error())
	}
	if request.ProofID != proof.ProofID {
		return failure(OutcomeDenied, "PROOF_IDENTITY_MISMATCH", "request proof id differs from proof")
	}
	if state.RootStatus == RootRetired {
		return failure(OutcomeRevoked, "TRUST_ROOT_RETIRED", "trust root incarnation is retired")
	}
	if state.RootStatus != RootActive {
		return failure(OutcomeUnknown, "TRUST_ROOT_MISSING", "no active trust root")
	}
	if state.TrustDomain != body.TrustDomain || state.RootIncarnation != body.RootIncarnation {
		return failure(OutcomeUnknown, "TRUST_ROOT_MISMATCH", "trust domain/root incarnation is not current")
	}
	now, err := parseUTC(state.TrustedNow)
	if err != nil {
		return failure(OutcomeUnknown, "TRUSTED_TIME_INVALID", err.Error())
	}
	if body.TrustDomain != request.TrustDomain || body.RootIncarnation != state.RootIncarnation {
		return failure(OutcomeDenied, "PROOF_TRUST_BINDING_MISMATCH", "proof trust domain/root differs from request")
	}
	if body.Version != ProofVersion {
		return failure(OutcomeDenied, "PROOF_VERSION_UNSUPPORTED", "proof version is not supported")
	}
	if body.Issuer != body.Subject {
		return failure(OutcomeDenied, "PROOF_ISSUER_SUBJECT_MISMATCH", "final proof signer subject differs from authenticated subject")
	}
	if !subjectMatches(body.Subject, request.Subject) ||
		body.Resource != request.Resource || body.Action != request.Action ||
		body.Tenant != request.Tenant || body.Operation != request.Operation ||
		body.CaseID != request.CaseID || body.Round != request.Round ||
		body.ExecutionGeneration != request.ExecutionGeneration {
		return failure(OutcomeStale, "PROOF_REQUEST_BINDING_MISMATCH", "proof subject/resource/action/tenant/operation/case/round/generation differs from request")
	}
	if !subjectUsesIdentitySource(body.Subject, state.IdentitySourceID, state.IdentitySourceVersion) {
		return failure(OutcomeDenied, "IDENTITY_SOURCE_MISMATCH", "proof subject is not from the fixed identity source/version")
	}
	if err := validateWindow(body.IssuedAt, body.ExpiresAt, now); err != nil {
		return failure(OutcomeExpired, "PROOF_EXPIRED", err.Error())
	}
	signer, result := findCredential(state.Credentials, body.Signer)
	if result.Outcome != "" {
		return result
	}
	if !subjectMatches(signer.Subject, body.Subject) {
		return failure(OutcomeDenied, "PROOF_SIGNER_SUBJECT_MISMATCH", "proof credential subject differs from proof subject")
	}
	if result := verifyCredentialWindow(signer, state, now); result.Outcome != "" {
		return result
	}
	if !windowCovers(signer.NotBefore, signer.ExpiresAt, body.IssuedAt, body.ExpiresAt) {
		return failure(OutcomeDenied, "PROOF_EXCEEDS_CREDENTIAL", "proof validity is not contained in the signer credential validity")
	}
	digest, canonicalProof, err := ProofDigest(body)
	if err != nil {
		return failure(OutcomeDenied, "PROOF_DIGEST_INVALID", err.Error())
	}
	if proof.ProofID != proofIDForDigest(digest) {
		return failure(OutcomeDenied, "PROOF_IDENTITY_MISMATCH", "proof id is not its C0 body identity")
	}
	sig, err := base64.StdEncoding.DecodeString(proof.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return failure(OutcomeDenied, "PROOF_SIGNATURE_INVALID", "signature is not base64 Ed25519 bytes")
	}
	pub, err := decodeCredentialKey(signer)
	if err != nil {
		return failure(OutcomeDenied, "PROOF_SIGNATURE_INVALID", err.Error())
	}
	msg := append([]byte(proofSignatureDomain), canonicalProof...)
	if !ed25519.Verify(pub, msg, sig) {
		return failure(OutcomeDenied, "PROOF_SIGNATURE_INVALID", "Ed25519 signature does not verify")
	}
	if result := verifyDelegationChain(body, state, now); result.Outcome != "" {
		return result
	}
	if result := verifyConsent(body, request, state, now); result.Outcome != "" {
		return result
	}
	if result := verifyScopeSet(body.Scopes, request.Scopes, state.Epochs); result.Outcome != "" {
		return result
	}
	return validResult()
}

func validateRequest(r VerificationRequest) error {
	required := []struct {
		name  string
		value string
	}{
		{"trust_domain", r.TrustDomain}, {"proof_id", r.ProofID},
		{"subject.subject_id", r.Subject.SubjectID},
		{"subject.identity_source_id", r.Subject.IdentitySourceID},
		{"subject.identity_source_version", r.Subject.IdentitySourceVersion},
		{"resource", r.Resource}, {"action", r.Action}, {"tenant", r.Tenant},
		{"operation", r.Operation}, {"case_id", r.CaseID},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if r.Round == 0 {
		return fmt.Errorf("round must be positive")
	}
	if err := validateScopeSet(r.Scopes); err != nil {
		return err
	}
	return nil
}

func parseUTC(s string) (time.Time, error) {
	t, err := time.Parse(timeLayoutUTC, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("time %q is not strict UTC milliseconds", s)
	}
	return t, nil
}

func windowCovers(outerNotBefore, outerExpiresAt, innerNotBefore, innerExpiresAt string) bool {
	outerNB, err := parseUTC(outerNotBefore)
	if err != nil {
		return false
	}
	outerExp, err := parseUTC(outerExpiresAt)
	if err != nil {
		return false
	}
	innerNB, err := parseUTC(innerNotBefore)
	if err != nil {
		return false
	}
	innerExp, err := parseUTC(innerExpiresAt)
	if err != nil {
		return false
	}
	return !outerNB.After(innerNB) && !innerExp.After(outerExp)
}

func validateWindowShape(notBefore, expiresAt string) error {
	nb, err := parseUTC(notBefore)
	if err != nil {
		return err
	}
	exp, err := parseUTC(expiresAt)
	if err != nil {
		return err
	}
	if !exp.After(nb) {
		return fmt.Errorf("expires_at must be after not_before")
	}
	return nil
}

func validateWindow(notBefore, expiresAt string, now time.Time) error {
	nb, err := parseUTC(notBefore)
	if err != nil {
		return err
	}
	exp, err := parseUTC(expiresAt)
	if err != nil {
		return err
	}
	if !exp.After(nb) {
		return fmt.Errorf("expires_at must be after not_before")
	}
	if now.Before(nb) {
		return fmt.Errorf("not yet valid")
	}
	if !now.Before(exp) {
		return fmt.Errorf("expired at %s", expiresAt)
	}
	return nil
}

func subjectMatches(a, b Subject) bool {
	return a.SubjectID != "" && a == b
}

func subjectUsesIdentitySource(s Subject, sourceID, sourceVersion string) bool {
	return s.IdentitySourceID == sourceID && s.IdentitySourceVersion == sourceVersion
}

func findCredential(credentials []Credential, ref CredentialRef) (Credential, Result) {
	var found *Credential
	for i := range credentials {
		if credentials[i].Ref == ref {
			if found != nil {
				return Credential{}, failure(OutcomeConflict, "CREDENTIAL_AMBIGUOUS", "more than one credential has the exact ref")
			}
			found = &credentials[i]
		}
	}
	if found == nil {
		return Credential{}, failure(OutcomeUnknown, "CREDENTIAL_UNKNOWN", "credential ref is not registered under the pinned root")
	}
	return *found, Result{}
}

func verifyCredentialWindow(c Credential, state TrustState, now time.Time) Result {
	if c.Algorithm != SignatureAlgorithmEd25519 {
		return failure(OutcomeDenied, "CREDENTIAL_ALGORITHM_UNSUPPORTED", "credential algorithm is not ed25519")
	}
	if !subjectUsesIdentitySource(c.Subject, state.IdentitySourceID, state.IdentitySourceVersion) {
		return failure(OutcomeDenied, "IDENTITY_SOURCE_MISMATCH", "credential subject is not from the fixed identity source/version")
	}
	switch c.Status {
	case CredentialActive:
	case CredentialRetired:
		return failure(OutcomeStale, "CREDENTIAL_RETIRED", "credential version has been rotated")
	case CredentialRevoked:
		return failure(OutcomeRevoked, "CREDENTIAL_REVOKED", "credential has been revoked")
	default:
		return failure(OutcomeUnknown, "CREDENTIAL_STATUS_UNKNOWN", "credential status is not recognized")
	}
	if err := validateWindow(c.NotBefore, c.ExpiresAt, now); err != nil {
		return failure(OutcomeExpired, "CREDENTIAL_EXPIRED", err.Error())
	}
	if _, err := decodeCredentialKey(c); err != nil {
		return failure(OutcomeDenied, "CREDENTIAL_KEY_INVALID", err.Error())
	}
	return Result{}
}

func decodeCredentialKey(c Credential) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("credential public key is not a base64 Ed25519 key")
	}
	return ed25519.PublicKey(raw), nil
}

func findOwnerPolicy(policies []OwnerPolicy, id, version string) (OwnerPolicy, Result) {
	var found *OwnerPolicy
	for i := range policies {
		if policies[i].PolicyID == id && policies[i].PolicyVersion == version {
			if found != nil {
				return OwnerPolicy{}, failure(OutcomeConflict, "OWNER_POLICY_AMBIGUOUS", "more than one owner policy has the exact id/version")
			}
			found = &policies[i]
		}
	}
	if found == nil {
		return OwnerPolicy{}, failure(OutcomeUnknown, "OWNER_POLICY_UNKNOWN", "owner policy id/version is not registered")
	}
	return *found, Result{}
}

func verifyConsent(body Proof, request VerificationRequest, state TrustState, now time.Time) Result {
	consent := body.Consent
	if consent.Body.Version != ConsentVersion {
		return failure(OutcomeDenied, "CONSENT_VERSION_UNSUPPORTED", "consent version is not supported")
	}
	cb := consent.Body
	if cb.TrustDomain != body.TrustDomain || cb.RootIncarnation != body.RootIncarnation ||
		cb.DelegatedSubject != body.Subject || cb.Resource != body.Resource ||
		cb.Action != body.Action || cb.Tenant != body.Tenant || cb.Operation != body.Operation ||
		cb.CaseID != body.CaseID || cb.Round != body.Round || !sameScopes(cb.Scopes, body.Scopes) {
		return failure(OutcomeDenied, "CONSENT_BINDING_MISMATCH", "owner consent is not for the exact proof request")
	}
	if err := validateWindow(cb.NotBefore, cb.ExpiresAt, now); err != nil {
		return failure(OutcomeExpired, "CONSENT_EXPIRED", err.Error())
	}
	if !windowCovers(cb.NotBefore, cb.ExpiresAt, body.IssuedAt, body.ExpiresAt) {
		return failure(OutcomeDenied, "PROOF_EXCEEDS_CONSENT", "proof validity is not contained in owner consent validity")
	}
	policy, result := findOwnerPolicy(state.Policies, cb.PolicyID, cb.PolicyVersion)
	if result.Outcome != "" {
		return result
	}
	if policy.RootIncarnation != body.RootIncarnation {
		return failure(OutcomeDenied, "OWNER_POLICY_ROOT_MISMATCH", "owner policy belongs to another root incarnation")
	}
	if policy.Status != PolicyActive {
		if policy.Status == PolicyRetired {
			return failure(OutcomeStale, "OWNER_POLICY_RETIRED", "owner policy version is retired")
		}
		return failure(OutcomeUnknown, "OWNER_POLICY_STATUS_UNKNOWN", "owner policy status is not recognized")
	}
	if err := validateWindow(policy.NotBefore, policy.ExpiresAt, now); err != nil {
		return failure(OutcomeExpired, "OWNER_POLICY_EXPIRED", err.Error())
	}
	if !windowCovers(policy.NotBefore, policy.ExpiresAt, body.IssuedAt, body.ExpiresAt) {
		return failure(OutcomeDenied, "PROOF_EXCEEDS_POLICY", "proof validity is not contained in the authenticated owner policy validity")
	}
	if !exactMember(policy.Resources, body.Resource) || !exactMember(policy.Actions, body.Action) ||
		!exactMember(policy.Tenants, body.Tenant) || !exactMember(policy.Operations, body.Operation) {
		return failure(OutcomeDenied, "OWNER_POLICY_SCOPE_MISMATCH", "owner policy does not cover the exact proof scope")
	}
	required, err := requiredOwnerApprovals(policy)
	if err != nil {
		return failure(OutcomeDenied, "OWNER_POLICY_INVALID", err.Error())
	}
	owners := make(map[Subject]struct{}, len(policy.Owners))
	for _, owner := range policy.Owners {
		if !subjectUsesIdentitySource(owner, state.IdentitySourceID, state.IdentitySourceVersion) {
			return failure(OutcomeDenied, "OWNER_IDENTITY_SOURCE_MISMATCH", "owner policy contains an untrusted identity source")
		}
		owners[owner] = struct{}{}
	}
	if uint64(len(consent.Approvals)) < required {
		return failure(OutcomeDenied, "OWNER_CONSENT_THRESHOLD_NOT_MET", "owner approvals are below the pre-approved threshold")
	}
	seen := make(map[Subject]struct{}, len(consent.Approvals))
	cbDigest := consentDigest(cb)
	for _, approval := range consent.Approvals {
		if _, ok := owners[approval.Owner]; !ok {
			return failure(OutcomeDenied, "OWNER_CONSENT_WRONG_OWNER", "approval owner is not in the exact owner policy")
		}
		if _, duplicate := seen[approval.Owner]; duplicate {
			return failure(OutcomeDenied, "OWNER_CONSENT_DUPLICATE", "duplicate approvals by one owner do not add threshold count")
		}
		cred, result := findCredential(state.Credentials, approval.Signer)
		if result.Outcome != "" {
			return result
		}
		if !subjectMatches(cred.Subject, approval.Owner) {
			return failure(OutcomeDenied, "OWNER_CONSENT_SIGNER_MISMATCH", "approval signer does not authenticate the approval owner")
		}
		if result := verifyCredentialWindow(cred, state, now); result.Outcome != "" {
			return result
		}
		if !windowCovers(cred.NotBefore, cred.ExpiresAt, body.IssuedAt, body.ExpiresAt) {
			return failure(OutcomeDenied, "PROOF_EXCEEDS_CREDENTIAL", "proof validity is not contained in an owner approval credential")
		}
		sig, err := base64.StdEncoding.DecodeString(approval.Signature)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return failure(OutcomeDenied, "OWNER_CONSENT_SIGNATURE_INVALID", "approval signature is not base64 Ed25519 bytes")
		}
		pub, err := decodeCredentialKey(cred)
		if err != nil {
			return failure(OutcomeDenied, "OWNER_CONSENT_SIGNATURE_INVALID", err.Error())
		}
		binding, err := canonicalStruct(struct {
			Owner  Subject       `json:"owner"`
			Signer CredentialRef `json:"signer"`
		}{Owner: approval.Owner, Signer: approval.Signer})
		if err != nil {
			return failure(OutcomeDenied, "OWNER_CONSENT_SIGNATURE_INVALID", err.Error())
		}
		msg := make([]byte, 0, len(consentApprovalDomain)+len(cbDigest)+1+len(binding.Bytes()))
		msg = append(msg, consentApprovalDomain...)
		msg = append(msg, cbDigest...)
		msg = append(msg, '\n')
		msg = append(msg, binding.Bytes()...)
		if !ed25519.Verify(pub, msg, sig) {
			return failure(OutcomeDenied, "OWNER_CONSENT_SIGNATURE_INVALID", "approval signature does not verify")
		}
		seen[approval.Owner] = struct{}{}
	}
	switch policy.Mode {
	case OwnerAll:
		if uint64(len(seen)) != uint64(len(policy.Owners)) {
			return failure(OutcomeDenied, "OWNER_CONSENT_THRESHOLD_NOT_MET", "OWNER_ALL requires every distinct policy owner")
		}
	case OwnerThreshold:
		if uint64(len(seen)) < required {
			return failure(OutcomeDenied, "OWNER_CONSENT_THRESHOLD_NOT_MET", "OWNER_THRESHOLD not met")
		}
	case OwnerSingleDelegate:
		if len(policy.Owners) != 1 || required != 1 || len(seen) != 1 {
			return failure(OutcomeConflict, "OWNER_SINGLE_DELEGATE_CONFLICT", "SINGLE_DELEGATE requires exactly one approved owner")
		}
	default:
		return failure(OutcomeDenied, "OWNER_POLICY_MODE_INVALID", "owner policy mode is not recognized")
	}
	return Result{}
}

func requiredOwnerApprovals(policy OwnerPolicy) (uint64, error) {
	if len(policy.Owners) == 0 {
		return 0, fmt.Errorf("owner set is empty")
	}
	switch policy.Mode {
	case OwnerAll, OwnerSingleDelegate:
		if policy.Threshold != 1 && policy.Mode == OwnerSingleDelegate {
			return 0, fmt.Errorf("SINGLE_DELEGATE threshold must be 1")
		}
		if policy.Mode == OwnerSingleDelegate && len(policy.Owners) != 1 {
			return 0, fmt.Errorf("SINGLE_DELEGATE owner set must contain exactly one owner")
		}
		if policy.Mode == OwnerAll && policy.Threshold != uint64(len(policy.Owners)) {
			return 0, fmt.Errorf("OWNER_ALL threshold must equal the owner count")
		}
		return uint64(len(policy.Owners)), nil
	case OwnerThreshold:
		if policy.Threshold == 0 || policy.Threshold > uint64(len(policy.Owners)) {
			return 0, fmt.Errorf("OWNER_THRESHOLD must be between 1 and the owner count")
		}
		return policy.Threshold, nil
	default:
		return 0, fmt.Errorf("unknown owner policy mode")
	}
}

func consentDigest(body ConsentBody) string {
	c, err := canonicalStruct(body)
	if err != nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(consentEvidenceDomain))
	h.Write(c.Bytes())
	return hex.EncodeToString(h.Sum(nil))
}

func verifyScopeSet(proofScopes, requestScopes []ScopedEpoch, epochs []EpochState) Result {
	if err := validateScopeSet(proofScopes); err != nil {
		return failure(OutcomeDenied, "PROOF_SCOPES_INVALID", err.Error())
	}
	if err := validateScopeSet(requestScopes); err != nil {
		return failure(OutcomeDenied, "REQUEST_SCOPES_INVALID", err.Error())
	}
	if !sameScopes(proofScopes, requestScopes) {
		return failure(OutcomeStale, "SCOPED_EPOCH_MISMATCH", "proof scopes differ from expected current scopes")
	}
	for i, want := range proofScopes {
		key := ScopeKey{Kind: want.Kind, ScopeID: want.ScopeID}
		var found *EpochState
		for j := range epochs {
			if epochs[j].Scope == key {
				if found != nil {
					return failure(OutcomeConflict, "SCOPED_EPOCH_AMBIGUOUS", "duplicate epoch row for a scope")
				}
				found = &epochs[j]
			}
		}
		if found == nil {
			return failure(OutcomeUnknown, "SCOPED_EPOCH_UNKNOWN", fmt.Sprintf("scope %s/%s is not registered", key.Kind, key.ScopeID))
		}
		if found.Revoked {
			return failure(OutcomeRevoked, "SCOPE_REVOKED", fmt.Sprintf("%s scope %s is revoked", key.Kind, key.ScopeID))
		}
		if found.Scope.Kind != want.Kind {
			return failure(OutcomeStale, "SCOPE_KIND_CONFUSION", "scope kind was substituted")
		}
		if found.Epoch != want.Epoch {
			return failure(OutcomeStale, "SCOPED_EPOCH_STALE", fmt.Sprintf("%s/%s epoch is %d, proof has %d", key.Kind, key.ScopeID, found.Epoch, want.Epoch))
		}
		_ = i
	}
	return Result{}
}

func validateScopeSet(scopes []ScopedEpoch) error {
	if len(scopes) < 3 || len(scopes) > 64 {
		return fmt.Errorf("scope set must contain at least the three required kinds")
	}
	seenKeys := make(map[ScopeKey]bool, len(scopes))
	counts := map[ScopeKind]int{}
	previous := ScopeKey{}
	for i, scope := range scopes {
		key := ScopeKey{Kind: scope.Kind, ScopeID: scope.ScopeID}
		if strings.TrimSpace(scope.ScopeID) == "" {
			return fmt.Errorf("scope %d has empty id", i)
		}
		switch scope.Kind {
		case ScopeAuthority, ScopeContext, ScopeCredential:
		default:
			return fmt.Errorf("scope %d has unknown kind %q", i, scope.Kind)
		}
		if seenKeys[key] {
			return fmt.Errorf("scope %s/%s is duplicated", key.Kind, key.ScopeID)
		}
		seenKeys[key] = true
		counts[scope.Kind]++
		if i > 0 && scopeKeyLess(key, previous) {
			return fmt.Errorf("scopes must be sorted by kind and scope id")
		}
		previous = key
	}
	for _, kind := range []ScopeKind{ScopeAuthority, ScopeContext, ScopeCredential} {
		if counts[kind] == 0 {
			return fmt.Errorf("missing %s scope", kind)
		}
	}
	return nil
}

func sameScopes(a, b []ScopedEpoch) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func scopeKeyLess(a, b ScopeKey) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ScopeID < b.ScopeID
}

func exactMember(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

func normalizeSet(values []string) ([]string, error) {
	out := append([]string(nil), values...)
	sort.Strings(out)
	for i, value := range out {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("set contains an empty value")
		}
		if i > 0 && out[i-1] == value {
			return nil, fmt.Errorf("set contains duplicate value %q", value)
		}
	}
	return out, nil
}

func sameStringOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isSubset(child, parent []string) bool {
	set := make(map[string]struct{}, len(parent))
	for _, value := range parent {
		set[value] = struct{}{}
	}
	for _, value := range child {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

func verifyDelegationChain(proof Proof, state TrustState, now time.Time) Result {
	issuer := state.GovernanceSubject
	parentResources := state.RootGrant.Resources
	parentActions := state.RootGrant.Actions
	parentTenants := state.RootGrant.Tenants
	var parentNotBefore, parentExpiresAt time.Time
	seenIDs := make(map[string]struct{}, len(proof.Delegations))
	seenSubjects := make(map[Subject]struct{}, len(proof.Delegations)*2)
	seenSubjects[state.GovernanceSubject] = struct{}{}
	for i, signed := range proof.Delegations {
		body := signed.Body
		if body.Version != DelegationVersion {
			return failure(OutcomeDenied, "DELEGATION_VERSION_UNSUPPORTED", fmt.Sprintf("hop %d version is not supported", i))
		}
		if body.DelegationID == "" {
			return failure(OutcomeDenied, "DELEGATION_INVALID", fmt.Sprintf("hop %d has empty id", i))
		}
		if _, duplicate := seenIDs[body.DelegationID]; duplicate {
			return failure(OutcomeConflict, "DELEGATION_CYCLE", fmt.Sprintf("hop %d repeats delegation id", i))
		}
		seenIDs[body.DelegationID] = struct{}{}
		if _, duplicate := seenSubjects[body.Delegate]; duplicate {
			return failure(OutcomeConflict, "DELEGATION_CYCLE", fmt.Sprintf("hop %d repeats delegate identity", i))
		}
		seenSubjects[body.Issuer] = struct{}{}
		seenSubjects[body.Delegate] = struct{}{}
		if body.TrustDomain != state.TrustDomain || body.RootIncarnation != state.RootIncarnation {
			return failure(OutcomeDenied, "DELEGATION_TRUST_BINDING_MISMATCH", fmt.Sprintf("hop %d is from another trust domain/root", i))
		}
		if !subjectMatches(body.Issuer, issuer) {
			return failure(OutcomeDenied, "DELEGATION_ISSUER_MISMATCH", fmt.Sprintf("hop %d issuer is not the prior delegate", i))
		}
		if !subjectUsesIdentitySource(body.Issuer, state.IdentitySourceID, state.IdentitySourceVersion) ||
			!subjectUsesIdentitySource(body.Delegate, state.IdentitySourceID, state.IdentitySourceVersion) {
			return failure(OutcomeDenied, "DELEGATION_IDENTITY_SOURCE_MISMATCH", fmt.Sprintf("hop %d uses an untrusted identity source", i))
		}
		if i > 0 && !proof.Delegations[i-1].Body.Delegable {
			return failure(OutcomeDenied, "DELEGATION_NOT_DELEGABLE", fmt.Sprintf("hop %d exceeds a non-delegable parent", i))
		}
		resources, err := normalizeSet(body.Resources)
		if err != nil || len(resources) == 0 || !sameStringOrder(resources, body.Resources) {
			return failure(OutcomeDenied, "DELEGATION_SCOPE_INVALID", fmt.Sprintf("hop %d resources must be sorted and unique", i))
		}
		actions, err := normalizeSet(body.Actions)
		if err != nil || len(actions) == 0 || !sameStringOrder(actions, body.Actions) {
			return failure(OutcomeDenied, "DELEGATION_SCOPE_INVALID", fmt.Sprintf("hop %d actions must be sorted and unique", i))
		}
		tenants, err := normalizeSet(body.Tenants)
		if err != nil || len(tenants) == 0 || !sameStringOrder(tenants, body.Tenants) {
			return failure(OutcomeDenied, "DELEGATION_SCOPE_INVALID", fmt.Sprintf("hop %d tenants must be sorted and unique", i))
		}
		if !isSubset(resources, parentResources) || !isSubset(actions, parentActions) || !isSubset(tenants, parentTenants) {
			return failure(OutcomeDenied, "DELEGATION_SCOPE_WIDENED", fmt.Sprintf("hop %d widens parent scope", i))
		}
		notBefore, err := parseUTC(body.NotBefore)
		if err != nil {
			return failure(OutcomeDenied, "DELEGATION_TIME_INVALID", err.Error())
		}
		expiresAt, err := parseUTC(body.ExpiresAt)
		if err != nil || !expiresAt.After(notBefore) || !now.Before(expiresAt) || now.Before(notBefore) {
			return failure(OutcomeExpired, "DELEGATION_EXPIRED", fmt.Sprintf("hop %d validity window does not contain trusted now", i))
		}
		if i > 0 && (notBefore.Before(parentNotBefore) || expiresAt.After(parentExpiresAt)) {
			return failure(OutcomeDenied, "DELEGATION_WINDOW_WIDENED", fmt.Sprintf("hop %d validity exceeds its parent", i))
		}
		parentNotBefore, parentExpiresAt = notBefore, expiresAt
		credential, result := findCredential(state.Credentials, body.IssuerCredential)
		if result.Outcome != "" {
			return result
		}
		if !subjectMatches(credential.Subject, body.Issuer) {
			return failure(OutcomeDenied, "DELEGATION_SIGNER_MISMATCH", fmt.Sprintf("hop %d credential does not authenticate issuer", i))
		}
		if result := verifyCredentialWindow(credential, state, now); result.Outcome != "" {
			return result
		}
		signature, err := base64.StdEncoding.DecodeString(signed.Signature)
		if err != nil || len(signature) != ed25519.SignatureSize {
			return failure(OutcomeDenied, "DELEGATION_SIGNATURE_INVALID", fmt.Sprintf("hop %d signature is not Ed25519", i))
		}
		canonicalBody, err := canonicalStruct(body)
		if err != nil {
			return failure(OutcomeDenied, "DELEGATION_INVALID", err.Error())
		}
		pub, err := decodeCredentialKey(credential)
		if err != nil {
			return failure(OutcomeDenied, "DELEGATION_SIGNATURE_INVALID", err.Error())
		}
		msg := append([]byte("magi:authority-delegation-signature:v1\n"), canonicalBody.Bytes()...)
		if !ed25519.Verify(pub, msg, signature) {
			return failure(OutcomeDenied, "DELEGATION_SIGNATURE_INVALID", fmt.Sprintf("hop %d signature does not verify", i))
		}
		issuer = body.Delegate
		parentResources, parentActions, parentTenants = resources, actions, tenants
	}
	if !subjectMatches(proof.Issuer, issuer) {
		return failure(OutcomeDenied, "PROOF_ISSUER_MISMATCH", "final proof issuer is not the authenticated delegate")
	}
	if len(proof.Delegations) > 0 {
		last := proof.Delegations[len(proof.Delegations)-1].Body
		if !exactMember(last.Resources, proof.Resource) || !exactMember(last.Actions, proof.Action) || !exactMember(last.Tenants, proof.Tenant) {
			return failure(OutcomeDenied, "PROOF_OUTSIDE_DELEGATION", "proof scope is outside the final delegation")
		}
		if !windowCovers(last.NotBefore, last.ExpiresAt, proof.IssuedAt, proof.ExpiresAt) {
			return failure(OutcomeDenied, "PROOF_EXCEEDS_DELEGATION", "proof validity is not contained in the final delegation validity")
		}
	}
	return Result{}
}
