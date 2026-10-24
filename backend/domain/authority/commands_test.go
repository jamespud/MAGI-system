package authority_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
)

func signRevocation(t *testing.T, body authority.RevocationCommandBody, seed byte) authority.SignedRevocationCommand {
	t.Helper()
	msg := append([]byte("magi:authority-revocation-command-signature:v1\n"), canonicalBytes(t, body)...)
	return authority.SignedRevocationCommand{
		Body: body, Signer: credentialFor(seed, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope").Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(fixedPrivateKey(seed), msg)),
	}
}

func TestRevocationCommandRequiresAuthenticatedGovernance(t *testing.T) {
	state, _, _, _ := proofFixture(t)
	body := authority.RevocationCommandBody{
		Version:     authority.RevocationCommandVersion,
		CommandID:   "revoke-context-1",
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		Scope:         authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"},
		ExpectedEpoch: 4, ExpectedRowVersion: 1,
		Reason:   "owner withdrawal",
		IssuedAt: "2026-10-24T09:30:00.000Z", ExpiresAt: "2026-10-24T09:40:00.000Z",
	}
	command := signRevocation(t, body, 1)
	result := authority.VerifyRevocationCommand(command, state)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	command.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	result = authority.VerifyRevocationCommand(command, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "REVOCATION_SIGNATURE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestCredentialRotationCommandBindsExactVersionAndKey(t *testing.T) {
	state, _, _, _ := proofFixture(t)
	old := state.Credentials[0]
	replacement := credentialFor(11, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope")
	replacement.Ref.CredentialVersion = 2
	body := authority.CredentialRotationCommandBody{
		Version:     authority.CredentialRotationCommandVersion,
		CommandID:   "rotate-governance-1",
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		OldCredential: old.Ref, NewCredential: replacement,
		ExpectedEpoch: 5, ExpectedRowVersion: 1,
		Reason:   "scheduled rotation",
		IssuedAt: "2026-10-24T09:30:00.000Z", ExpiresAt: "2026-10-24T09:40:00.000Z",
	}
	msg := append([]byte("magi:authority-credential-rotation-command-signature:v1\n"), canonicalBytes(t, body)...)
	command := authority.SignedCredentialRotationCommand{
		Body: body, Signer: old.Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(fixedPrivateKey(1), msg)),
	}
	result := authority.VerifyCredentialRotationCommand(command, state)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	command.Body.NewCredential.Ref.CredentialVersion = 3
	msg = append([]byte("magi:authority-credential-rotation-command-signature:v1\n"), canonicalBytes(t, command.Body)...)
	command.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(fixedPrivateKey(1), msg))
	command.Body.NewCredential.Ref.CredentialVersion = 4 // signature covers 3, statement has 4
	result = authority.VerifyCredentialRotationCommand(command, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROTATION_SIGNATURE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestRevocationCommandRejectsNonGovernanceSigner(t *testing.T) {
	state, _, _, _ := proofFixture(t)
	body := authority.RevocationCommandBody{
		Version: authority.RevocationCommandVersion, CommandID: "revoke-owner-signed",
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		Scope:         authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"},
		ExpectedEpoch: 4, ExpectedRowVersion: 1, Reason: "not governance",
		IssuedAt: "2026-10-24T09:30:00.000Z", ExpiresAt: "2026-10-24T09:40:00.000Z",
	}
	owner := state.Credentials[1]
	msg := append([]byte("magi:authority-revocation-command-signature:v1\n"), canonicalBytes(t, body)...)
	command := authority.SignedRevocationCommand{
		Body: body, Signer: owner.Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(fixedPrivateKey(2), msg)),
	}
	result := authority.VerifyRevocationCommand(command, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "COMMAND_SIGNER_NOT_GOVERNANCE" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}
