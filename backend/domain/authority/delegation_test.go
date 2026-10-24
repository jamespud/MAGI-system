package authority_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
)

func signDelegation(t *testing.T, body authority.DelegationBody, seed byte) authority.SignedDelegation {
	t.Helper()
	msg := append([]byte("magi:authority-delegation-signature:v1\n"), canonicalBytes(t, body)...)
	sig := ed25519.Sign(fixedPrivateKey(seed), msg)
	return authority.SignedDelegation{Body: body, Signature: base64.StdEncoding.EncodeToString(sig)}
}

func delegationFixture(t *testing.T) (authority.TrustState, authority.VerificationRequest, []byte, string) {
	t.Helper()
	state, _, _, _ := proofFixture(t)
	governance := credentialFor(1, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope")
	delegate := credentialFor(3, "cred-delegate", "delegate-a", "identity-source", "identity-v1", "credential-scope")
	owner := credentialFor(2, "cred-owner", "owner-a", "identity-source", "identity-v1", "credential-scope")
	state.Credentials = []authority.Credential{governance, owner, delegate}
	scopes := []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	hop := signDelegation(t, authority.DelegationBody{
		Version: authority.DelegationVersion, DelegationID: "hop-1",
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		Issuer: governance.Subject, IssuerCredential: governance.Ref,
		Delegate:  delegate.Subject,
		Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"},
		NotBefore: "2026-10-24T09:00:00.000Z", ExpiresAt: "2026-10-24T12:00:00.000Z", Delegable: false,
	}, 1)
	policy := state.Policies[0]
	consentBody := authority.ConsentBody{
		Version: authority.ConsentVersion, ConsentID: "consent-delegated",
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		PolicyID: policy.PolicyID, PolicyVersion: policy.PolicyVersion,
		DelegatedSubject: delegate.Subject,
		Resource:         "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, Scopes: scopes,
		NotBefore: "2026-10-24T09:00:00.000Z", ExpiresAt: "2026-10-24T12:00:00.000Z",
	}
	consent := signConsent(t, consentBody, struct {
		owner  authority.Subject
		signer authority.CredentialRef
		seed   byte
	}{owner: owner.Subject, signer: owner.Ref, seed: 2})
	proof := authority.Proof{
		Version: authority.ProofVersion, TrustDomain: "tenant-a", RootIncarnation: "root-1",
		Issuer: delegate.Subject, Subject: delegate.Subject,
		Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, ExecutionGeneration: 7, Scopes: scopes,
		IssuedAt: "2026-10-24T09:30:00.000Z", ExpiresAt: "2026-10-24T09:40:00.000Z",
		Signer: delegate.Ref, Delegations: []authority.SignedDelegation{hop}, Consent: consent,
	}
	raw, id := signProof(t, proof, 3)
	req := authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: id, Subject: delegate.Subject,
		Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, ExecutionGeneration: 7, Scopes: scopes,
	}
	return state, req, raw, id
}

func TestDelegationAuthenticatedChainValid(t *testing.T) {
	state, req, raw, id := delegationFixture(t)
	parsed, result := authority.ParseSignedProof(raw, id)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("parse outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	result = authority.VerifySignedProof(parsed, req, state)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("verify outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}
