package authority

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const (
	revocationCommandSignatureDomain = "magi:authority-revocation-command-signature:v1\n"
	rotationCommandSignatureDomain   = "magi:authority-credential-rotation-command-signature:v1\n"
)

// VerifyRevocationCommand authenticates a governance command for one exact
// scoped-epoch CAS. It does not mutate state.
func VerifyRevocationCommand(command SignedRevocationCommand, state TrustState) Result {
	body := command.Body
	if body.Version != RevocationCommandVersion {
		return failure(OutcomeDenied, "REVOCATION_COMMAND_VERSION_UNSUPPORTED", "revocation command version is not supported")
	}
	if strings.TrimSpace(body.CommandID) == "" || body.ExpectedRowVersion == 0 || !validScopeKey(body.Scope) {
		return failure(OutcomeDenied, "REVOCATION_COMMAND_INVALID", "command id, scope, and expected row version are required")
	}
	if body.TrustDomain != state.TrustDomain || body.RootIncarnation != state.RootIncarnation {
		return failure(OutcomeDenied, "REVOCATION_COMMAND_TRUST_MISMATCH", "command trust domain/root is not current")
	}
	now, result := authenticateCommandWindow(body.IssuedAt, body.ExpiresAt, command.Signer, state)
	if result.Outcome != "" {
		return result
	}
	msg, err := commandSignatureBytes(revocationCommandSignatureDomain, body)
	if err != nil {
		return failure(OutcomeDenied, "REVOCATION_COMMAND_INVALID", err.Error())
	}
	if result := verifyGovernanceCommandSignature(command.Signer, command.Signature, msg, state, now, "REVOCATION_SIGNATURE_INVALID"); result.Outcome != "" {
		return result
	}
	return Result{Outcome: OutcomeValid, Code: "REVOCATION_COMMAND_AUTHENTICATED"}
}

// VerifyCredentialRotationCommand authenticates the exact old version, new key,
// scoped epoch CAS, and governance signature. It does not mutate state.
func VerifyCredentialRotationCommand(command SignedCredentialRotationCommand, state TrustState) Result {
	body := command.Body
	if body.Version != CredentialRotationCommandVersion {
		return failure(OutcomeDenied, "CREDENTIAL_ROTATION_COMMAND_VERSION_UNSUPPORTED", "credential rotation command version is not supported")
	}
	if strings.TrimSpace(body.CommandID) == "" || body.ExpectedRowVersion == 0 {
		return failure(OutcomeDenied, "CREDENTIAL_ROTATION_COMMAND_INVALID", "command id and expected row version are required")
	}
	if body.TrustDomain != state.TrustDomain || body.RootIncarnation != state.RootIncarnation {
		return failure(OutcomeDenied, "CREDENTIAL_ROTATION_COMMAND_TRUST_MISMATCH", "command trust domain/root is not current")
	}
	now, result := authenticateCommandWindow(body.IssuedAt, body.ExpiresAt, command.Signer, state)
	if result.Outcome != "" {
		return result
	}
	msg, err := commandSignatureBytes(rotationCommandSignatureDomain, body)
	if err != nil {
		return failure(OutcomeDenied, "CREDENTIAL_ROTATION_COMMAND_INVALID", err.Error())
	}
	if result := verifyGovernanceCommandSignature(command.Signer, command.Signature, msg, state, now, "CREDENTIAL_ROTATION_SIGNATURE_INVALID"); result.Outcome != "" {
		return result
	}
	if err := validateCredentialReplacement(body.OldCredential, body.NewCredential, state, now); err != nil {
		return failure(OutcomeDenied, "CREDENTIAL_ROTATION_COMMAND_INVALID", err.Error())
	}
	return Result{Outcome: OutcomeValid, Code: "CREDENTIAL_ROTATION_COMMAND_AUTHENTICATED"}
}

func authenticateCommandWindow(notBefore, expiresAt string, signerRef CredentialRef, state TrustState) (time.Time, Result) {
	if state.RootStatus == RootRetired {
		return time.Time{}, failure(OutcomeRevoked, "TRUST_ROOT_RETIRED", "trust root is retired")
	}
	if state.RootStatus != RootActive {
		return time.Time{}, failure(OutcomeUnknown, "TRUST_ROOT_MISSING", "no active trust root")
	}
	now, err := parseUTC(state.TrustedNow)
	if err != nil {
		return time.Time{}, failure(OutcomeUnknown, "TRUSTED_TIME_INVALID", err.Error())
	}
	if err := validateWindow(notBefore, expiresAt, now); err != nil {
		return time.Time{}, failure(OutcomeExpired, "COMMAND_EXPIRED", err.Error())
	}
	signer, result := findCredential(state.Credentials, signerRef)
	if result.Outcome != "" {
		return time.Time{}, result
	}
	if !subjectMatches(signer.Subject, state.GovernanceSubject) {
		return time.Time{}, failure(OutcomeDenied, "COMMAND_SIGNER_NOT_GOVERNANCE", "command must be signed by the authenticated governance subject")
	}
	if result := verifyCredentialWindow(signer, state, now); result.Outcome != "" {
		return time.Time{}, result
	}
	if !windowCovers(signer.NotBefore, signer.ExpiresAt, notBefore, expiresAt) {
		return time.Time{}, failure(OutcomeDenied, "COMMAND_EXCEEDS_CREDENTIAL", "command validity exceeds the governance credential")
	}
	return now, Result{}
}

func verifyGovernanceCommandSignature(signerRef CredentialRef, signature string, msg []byte, state TrustState, now time.Time, code string) Result {
	signer, result := findCredential(state.Credentials, signerRef)
	if result.Outcome != "" {
		return result
	}
	if !subjectMatches(signer.Subject, state.GovernanceSubject) {
		return failure(OutcomeDenied, "COMMAND_SIGNER_NOT_GOVERNANCE", "command must be signed by the authenticated governance subject")
	}
	if result := verifyCredentialWindow(signer, state, now); result.Outcome != "" {
		return result
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return failure(OutcomeDenied, code, "signature is not base64 Ed25519 bytes")
	}
	pub, err := decodeCredentialKey(signer)
	if err != nil {
		return failure(OutcomeDenied, code, err.Error())
	}
	if !ed25519.Verify(pub, msg, sig) {
		return failure(OutcomeDenied, code, "Ed25519 signature does not verify")
	}
	return Result{}
}

func commandSignatureBytes(domain string, body any) ([]byte, error) {
	c, err := canonicalStruct(body)
	if err != nil {
		return nil, err
	}
	return append([]byte(domain), c.Bytes()...), nil
}

func validateCredentialReplacement(oldRef CredentialRef, replacement Credential, state TrustState, now time.Time) error {
	old, result := findCredential(state.Credentials, oldRef)
	if result.Outcome != "" {
		return fmt.Errorf("old credential is not registered")
	}
	if old.Ref.RootIncarnation != state.RootIncarnation {
		return fmt.Errorf("old credential belongs to another root incarnation")
	}
	if old.Status != CredentialActive {
		return fmt.Errorf("old credential is not active")
	}
	if replacement.Ref.RootIncarnation != state.RootIncarnation ||
		replacement.Ref.CredentialID != old.Ref.CredentialID ||
		replacement.Ref.CredentialVersion != old.Ref.CredentialVersion+1 {
		return fmt.Errorf("replacement must be the exact next version of the same credential")
	}
	if replacement.Subject != old.Subject || replacement.Scope != old.Scope ||
		replacement.Algorithm != SignatureAlgorithmEd25519 || replacement.Status != CredentialActive {
		return fmt.Errorf("replacement must preserve subject, scope, algorithm, and active status")
	}
	if replacement.PublicKey == old.PublicKey {
		return fmt.Errorf("replacement public key must differ")
	}
	if err := validateWindow(replacement.NotBefore, replacement.ExpiresAt, now); err != nil {
		return err
	}
	if _, err := decodeCredentialKey(replacement); err != nil {
		return err
	}
	return nil
}

func validScopeKey(scope ScopeKey) bool {
	if strings.TrimSpace(scope.ScopeID) == "" {
		return false
	}
	switch scope.Kind {
	case ScopeAuthority, ScopeContext, ScopeCredential:
		return true
	default:
		return false
	}
}
