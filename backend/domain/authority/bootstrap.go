package authority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/canonical"
)

const (
	bootstrapSignatureDomain = "magi:authority-bootstrap-signature:v1\n"
	bootstrapRootDomain      = "magi:authority-root:v1\n"
)

// AuthenticateBootstrap verifies a one-time out-of-band root installation
// envelope against a deployment-pinned recovery key and fixed identity source.
// The zero BootstrapTrust has no key and therefore returns UNKNOWN; this package
// never supplies a production recovery key or governance root.
func AuthenticateBootstrap(raw []byte, trust BootstrapTrust) (TrustBootstrap, string, Result) {
	return authenticateBootstrap(raw, trust, true)
}

func authenticateBootstrap(raw []byte, trust BootstrapTrust, enforceCurrentValidity bool) (TrustBootstrap, string, Result) {
	envelope, result := parseBootstrapEnvelope(raw)
	if result.Outcome != "" {
		return TrustBootstrap{}, "", result
	}
	if strings.TrimSpace(trust.PublicKey) == "" || strings.TrimSpace(trust.RecoveryKeyID) == "" ||
		trust.RecoveryKeyVersion == 0 || trust.Algorithm != SignatureAlgorithmEd25519 {
		return TrustBootstrap{}, "", failure(OutcomeUnknown, "BOOTSTRAP_TRUST_MISSING", "deployment-pinned recovery trust is not configured")
	}
	if envelope.KeyID != trust.RecoveryKeyID || envelope.KeyVersion != trust.RecoveryKeyVersion ||
		envelope.Algorithm != trust.Algorithm {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_RECOVERY_KEY_MISMATCH", "bootstrap envelope does not use the exact pinned recovery key/version")
	}
	body := envelope.Bootstrap
	if body.Version != BootstrapVersion {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_VERSION_UNSUPPORTED", "bootstrap version is not supported")
	}
	if body.IdentitySourceID != trust.IdentitySourceID || body.IdentitySourceVersion != trust.IdentitySourceVersion {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_IDENTITY_SOURCE_MISMATCH", "bootstrap identity source/version is not the deployment-pinned source")
	}
	recoveryKey, err := base64.StdEncoding.DecodeString(trust.PublicKey)
	if err != nil || len(recoveryKey) != ed25519.PublicKeySize {
		return TrustBootstrap{}, "", failure(OutcomeUnknown, "BOOTSTRAP_TRUST_MISSING", "pinned recovery public key is invalid")
	}
	canonicalBody, err := canonicalStruct(body)
	if err != nil {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_INVALID", err.Error())
	}
	sig, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_SIGNATURE_INVALID", "bootstrap signature is not base64 Ed25519 bytes")
	}
	msg := append([]byte(bootstrapSignatureDomain), canonicalBody.Bytes()...)
	if !ed25519.Verify(ed25519.PublicKey(recoveryKey), msg, sig) {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_SIGNATURE_INVALID", "bootstrap Ed25519 signature does not verify")
	}
	if result := validateBootstrap(body, trust, enforceCurrentValidity); result.Outcome != "" {
		return TrustBootstrap{}, "", result
	}
	fingerprint := bootstrapFingerprint(canonicalBody.Bytes())
	return body, fingerprint, Result{Outcome: OutcomeValid, Code: "BOOTSTRAP_AUTHENTICATED"}
}

// ReadBootstrap re-authenticates stored original envelope bytes and requires the
// exact pinned fingerprint. It never repairs or re-canonicalizes a stored row.
func ReadBootstrap(raw []byte, expectedFingerprint string, trust BootstrapTrust) (TrustBootstrap, string, Result) {
	if strings.TrimSpace(expectedFingerprint) == "" {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_FINGERPRINT_REQUIRED", "readback requires the pinned root fingerprint")
	}
	body, fingerprint, result := authenticateBootstrap(raw, trust, false)
	if result.Outcome != OutcomeValid {
		return TrustBootstrap{}, "", result
	}
	if fingerprint != expectedFingerprint {
		return TrustBootstrap{}, "", failure(OutcomeDenied, "BOOTSTRAP_FINGERPRINT_MISMATCH", "stored bootstrap fingerprint differs from the pinned root")
	}
	return body, fingerprint, Result{Outcome: OutcomeValid, Code: "BOOTSTRAP_READBACK_VALID"}
}

func parseBootstrapEnvelope(raw []byte) (BootstrapEnvelope, Result) {
	c, err := canonical.CanonicalizeWithLimits(raw, proofLimits)
	if err != nil {
		return BootstrapEnvelope{}, failure(OutcomeDenied, "BOOTSTRAP_BYTES_INVALID", err.Error())
	}
	if !bytes.Equal(c.Bytes(), raw) {
		return BootstrapEnvelope{}, failure(OutcomeDenied, "BOOTSTRAP_BYTES_NON_CANONICAL", "stored bootstrap bytes must already be canonical")
	}
	var envelope BootstrapEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return BootstrapEnvelope{}, failure(OutcomeDenied, "BOOTSTRAP_SHAPE_INVALID", err.Error())
	}
	reencoded, err := canonicalStruct(envelope)
	if err != nil || !bytes.Equal(reencoded.Bytes(), raw) {
		return BootstrapEnvelope{}, failure(OutcomeDenied, "BOOTSTRAP_SHAPE_INVALID", "bootstrap contains missing or unknown fields")
	}
	return envelope, Result{}
}

func validateBootstrap(body TrustBootstrap, trust BootstrapTrust, enforceCurrentValidity bool) Result {
	if strings.TrimSpace(body.TrustDomain) == "" || strings.TrimSpace(body.RootIncarnation) == "" ||
		strings.TrimSpace(body.IdentitySourceID) == "" || strings.TrimSpace(body.IdentitySourceVersion) == "" ||
		strings.TrimSpace(body.GovernanceSubject.SubjectID) == "" {
		return failure(OutcomeDenied, "BOOTSTRAP_INVALID", "trust domain, root incarnation, identity source, and governance subject are required")
	}
	if !subjectUsesIdentitySource(body.GovernanceSubject, trust.IdentitySourceID, trust.IdentitySourceVersion) {
		return failure(OutcomeDenied, "BOOTSTRAP_IDENTITY_SOURCE_MISMATCH", "governance subject uses a different identity source/version")
	}
	for name, values := range map[string][]string{
		"root resources":  body.RootGrant.Resources,
		"root actions":    body.RootGrant.Actions,
		"root tenants":    body.RootGrant.Tenants,
		"root operations": body.RootGrant.Operations,
	} {
		if result := requireCanonicalNonEmptySet(name, values); result.Outcome != "" {
			return result
		}
	}
	var now time.Time
	if enforceCurrentValidity {
		var err error
		now, err = parseUTC(trust.TrustedNow)
		if err != nil {
			return failure(OutcomeUnknown, "BOOTSTRAP_TRUSTED_TIME_INVALID", err.Error())
		}
	}
	state := TrustState{
		TrustDomain: body.TrustDomain, RootIncarnation: body.RootIncarnation,
		RootStatus: RootActive, IdentitySourceID: trust.IdentitySourceID,
		IdentitySourceVersion: trust.IdentitySourceVersion,
		GovernanceSubject:     body.GovernanceSubject, RootGrant: body.RootGrant,
		TrustedNow: trust.TrustedNow,
	}
	if len(body.Credentials) == 0 {
		return failure(OutcomeDenied, "BOOTSTRAP_INVALID", "bootstrap must register at least one credential")
	}
	refs := make(map[CredentialRef]struct{}, len(body.Credentials))
	subjects := make(map[Subject]struct{}, len(body.Credentials))
	governanceKeys := 0
	for i, credential := range body.Credentials {
		if credential.Ref.RootIncarnation != body.RootIncarnation || credential.Ref.CredentialVersion == 0 ||
			strings.TrimSpace(credential.Ref.CredentialID) == "" {
			return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_INVALID", fmt.Sprintf("credential %d ref is invalid", i))
		}
		if _, duplicate := refs[credential.Ref]; duplicate {
			return failure(OutcomeConflict, "BOOTSTRAP_CREDENTIAL_AMBIGUOUS", fmt.Sprintf("credential %d repeats an exact ref", i))
		}
		refs[credential.Ref] = struct{}{}
		if _, duplicate := subjects[credential.Subject]; duplicate {
			return failure(OutcomeConflict, "BOOTSTRAP_IDENTITY_AMBIGUOUS", fmt.Sprintf("credential %d gives one subject multiple active keys", i))
		}
		subjects[credential.Subject] = struct{}{}
		if !subjectUsesIdentitySource(credential.Subject, trust.IdentitySourceID, trust.IdentitySourceVersion) {
			return failure(OutcomeDenied, "BOOTSTRAP_IDENTITY_SOURCE_MISMATCH", fmt.Sprintf("credential %d uses an untrusted identity source", i))
		}
		if credential.Scope.Kind != ScopeCredential || strings.TrimSpace(credential.Scope.ScopeID) == "" {
			return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_SCOPE_INVALID", fmt.Sprintf("credential %d must carry a non-empty CREDENTIAL scope", i))
		}
		if enforceCurrentValidity {
			if result := verifyCredentialWindow(credential, state, now); result.Outcome != "" {
				return result
			}
		} else {
			if credential.Status != CredentialActive || credential.Algorithm != SignatureAlgorithmEd25519 {
				return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_INVALID", fmt.Sprintf("credential %d has unsupported status or algorithm", i))
			}
			if _, err := decodeCredentialKey(credential); err != nil {
				return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_INVALID", err.Error())
			}
			if err := validateWindowShape(credential.NotBefore, credential.ExpiresAt); err != nil {
				return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_INVALID", err.Error())
			}
		}
		if credential.Subject == body.GovernanceSubject {
			governanceKeys++
		}
	}
	if governanceKeys != 1 {
		return failure(OutcomeConflict, "BOOTSTRAP_GOVERNANCE_AMBIGUOUS", "governance subject must have exactly one active credential")
	}
	if result := validateInitialScopes(body.InitialScopes); result.Outcome != "" {
		return result
	}
	for _, credential := range body.Credentials {
		found := false
		for _, scope := range body.InitialScopes {
			if scope.Kind == credential.Scope.Kind && scope.ScopeID == credential.Scope.ScopeID {
				found = true
				break
			}
		}
		if !found {
			return failure(OutcomeDenied, "BOOTSTRAP_CREDENTIAL_SCOPE_MISSING", "credential scope has no initial scoped epoch")
		}
	}
	policies := make(map[string]struct{}, len(body.OwnerPolicies))
	for i, policy := range body.OwnerPolicies {
		if policy.RootIncarnation != body.RootIncarnation {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", fmt.Sprintf("policy %d belongs to another root incarnation", i))
		}
		if strings.TrimSpace(policy.PolicyID) == "" || strings.TrimSpace(policy.PolicyVersion) == "" {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", fmt.Sprintf("policy %d id/version is empty", i))
		}
		key := policy.PolicyID + "\x00" + policy.PolicyVersion
		if _, duplicate := policies[key]; duplicate {
			return failure(OutcomeConflict, "BOOTSTRAP_OWNER_POLICY_AMBIGUOUS", fmt.Sprintf("policy %d repeats an exact id/version", i))
		}
		policies[key] = struct{}{}
		if policy.Status != PolicyActive {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", "bootstrap policies must be active")
		}
		if len(policy.Owners) > MaxPolicyOwners {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_LIMIT_EXCEEDED", "owner policy exceeds 64 owners")
		}
		if _, err := requiredOwnerApprovals(policy); err != nil {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", err.Error())
		}
		if enforceCurrentValidity {
			if err := validateWindow(policy.NotBefore, policy.ExpiresAt, now); err != nil {
				return failure(OutcomeExpired, "BOOTSTRAP_OWNER_POLICY_EXPIRED", err.Error())
			}
		} else if err := validateWindowShape(policy.NotBefore, policy.ExpiresAt); err != nil {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", err.Error())
		}
		seenOwners := make(map[Subject]struct{}, len(policy.Owners))
		for _, owner := range policy.Owners {
			if _, ok := subjects[owner]; !ok {
				return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_INVALID", "policy owner has no authenticated credential")
			}
			if _, duplicate := seenOwners[owner]; duplicate {
				return failure(OutcomeConflict, "BOOTSTRAP_OWNER_POLICY_AMBIGUOUS", "policy repeats an owner identity")
			}
			seenOwners[owner] = struct{}{}
		}
		for name, values := range map[string][]string{
			"policy resources": policy.Resources, "policy actions": policy.Actions,
			"policy tenants": policy.Tenants, "policy operations": policy.Operations,
		} {
			if result := requireCanonicalNonEmptySet(name, values); result.Outcome != "" {
				return result
			}
		}
		if !isSubset(policy.Resources, body.RootGrant.Resources) ||
			!isSubset(policy.Actions, body.RootGrant.Actions) ||
			!isSubset(policy.Tenants, body.RootGrant.Tenants) ||
			!isSubset(policy.Operations, body.RootGrant.Operations) {
			return failure(OutcomeDenied, "BOOTSTRAP_OWNER_POLICY_OUTSIDE_ROOT_GRANT", "owner policy exceeds the pinned root grant")
		}
	}
	return Result{}
}

func validateInitialScopes(scopes []ScopedEpoch) Result {
	if err := validateScopeSet(scopes); err != nil {
		return failure(OutcomeDenied, "BOOTSTRAP_SCOPE_INVALID", err.Error())
	}
	return Result{}
}

func requireCanonicalNonEmptySet(name string, values []string) Result {
	if len(values) == 0 {
		return failure(OutcomeDenied, "BOOTSTRAP_SCOPE_INVALID", name+" must not be empty")
	}
	normalized, err := normalizeSet(values)
	if err != nil {
		return failure(OutcomeDenied, "BOOTSTRAP_SCOPE_INVALID", name+": "+err.Error())
	}
	if len(normalized) != len(values) {
		return failure(OutcomeConflict, "BOOTSTRAP_SCOPE_INVALID", name+" is ambiguous")
	}
	for i := range values {
		if values[i] != normalized[i] {
			return failure(OutcomeDenied, "BOOTSTRAP_SCOPE_INVALID", name+" must be sorted")
		}
	}
	return Result{}
}

func bootstrapFingerprint(canonicalBody []byte) string {
	h := sha256.New()
	h.Write([]byte(bootstrapRootDomain))
	h.Write(canonicalBody)
	return hex.EncodeToString(h.Sum(nil))
}
