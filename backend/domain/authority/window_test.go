package authority_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
)

func TestProofValidityIsBoundedByEveryAuthorityWindow(t *testing.T) {
	t.Run("credential", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.TrustedNow = "2026-10-24T11:45:00.000Z"
		proof.Proof.IssuedAt = "2026-10-24T11:30:00.000Z"
		proof.Proof.ExpiresAt = "2026-10-24T12:30:00.000Z"
		proof.Proof.Consent.Body.NotBefore = "2026-10-24T11:00:00.000Z"
		proof.Proof.Consent.Body.ExpiresAt = "2026-10-24T13:00:00.000Z"
		state.Policies[0].ExpiresAt = "2026-10-24T13:00:00.000Z"
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_EXCEEDS_CREDENTIAL" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner consent", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.TrustedNow = "2026-10-24T11:45:00.000Z"
		proof.Proof.IssuedAt = "2026-10-24T11:40:00.000Z"
		proof.Proof.ExpiresAt = "2026-10-24T12:00:00.000Z"
		proof.Proof.Consent.Body.NotBefore = "2026-10-24T11:00:00.000Z"
		proof.Proof.Consent.Body.ExpiresAt = "2026-10-24T11:50:00.000Z"
		state.Policies[0].ExpiresAt = "2026-10-24T13:00:00.000Z"
		state.Credentials[0].ExpiresAt = "2026-10-24T13:00:00.000Z"
		state.Credentials[1].ExpiresAt = "2026-10-24T13:00:00.000Z"
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_EXCEEDS_CONSENT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner policy", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.TrustedNow = "2026-10-24T11:45:00.000Z"
		proof.Proof.IssuedAt = "2026-10-24T11:40:00.000Z"
		proof.Proof.ExpiresAt = "2026-10-24T12:00:00.000Z"
		proof.Proof.Consent.Body.NotBefore = "2026-10-24T11:00:00.000Z"
		proof.Proof.Consent.Body.ExpiresAt = "2026-10-24T13:00:00.000Z"
		state.Policies[0].ExpiresAt = "2026-10-24T11:50:00.000Z"
		state.Credentials[0].ExpiresAt = "2026-10-24T13:00:00.000Z"
		state.Credentials[1].ExpiresAt = "2026-10-24T13:00:00.000Z"
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_EXCEEDS_POLICY" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestDelegationRequiresStableSetEncoding(t *testing.T) {
	state, req, raw, id := delegationFixture(t)
	proof := mustParseProof(t, raw, id)
	state.RootGrant.Tenants = []string{"tenant-a", "tenant-b"}
	hop := proof.Proof.Delegations[0]
	hop.Body.Tenants = []string{"tenant-b", "tenant-a"}
	proof.Proof.Delegations[0] = signDelegation(t, hop.Body, 1)
	reSignFixture(t, &proof, &req, 3)
	result := authority.VerifySignedProof(proof, req, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "DELEGATION_SCOPE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestOwnerPolicyIsPinnedToRootIncarnation(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	proof := mustParseProof(t, raw, id)
	state.Policies[0].RootIncarnation = "root-other"
	result := authority.VerifySignedProof(proof, req, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_POLICY_ROOT_MISMATCH" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}
