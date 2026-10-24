package authority_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
)

const (
	proofSigningDomain    = "magi:authority-proof-signature:v1\n"
	consentApprovalDomain = "magi:authority-consent-approval:v1\n"
)

func fixedPrivateKey(seed byte) ed25519.PrivateKey {
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed
	}
	return ed25519.NewKeyFromSeed(raw)
}

func credentialFor(seed byte, id, subject, sourceID, sourceVersion, scopeID string) authority.Credential {
	pub := fixedPrivateKey(seed).Public().(ed25519.PublicKey)
	return authority.Credential{
		Ref: authority.CredentialRef{
			RootIncarnation:   "root-1",
			CredentialID:      id,
			CredentialVersion: 1,
		},
		Subject: authority.Subject{
			SubjectID:             subject,
			IdentitySourceID:      sourceID,
			IdentitySourceVersion: sourceVersion,
		},
		Algorithm: authority.SignatureAlgorithmEd25519,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		NotBefore: "2026-10-24T09:00:00.000Z",
		ExpiresAt: "2026-10-24T12:00:00.000Z",
		Status:    authority.CredentialActive,
		Scope:     authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: scopeID},
	}
}

func canonicalBytes(t *testing.T, v any) []byte {
	t.Helper()
	c, err := canonical.CanonicalizeStruct(v)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	return c.Bytes()
}

func consentDigest(t *testing.T, body authority.ConsentBody) string {
	t.Helper()
	h := sha256.New()
	h.Write([]byte("magi:authority-consent-evidence:v1\n"))
	h.Write(canonicalBytes(t, body))
	return hex.EncodeToString(h.Sum(nil))
}

func consentApprovalBytes(t *testing.T, body authority.ConsentBody, owner authority.Subject, signer authority.CredentialRef) []byte {
	t.Helper()
	var b strings.Builder
	b.WriteString(consentApprovalDomain)
	b.WriteString(consentDigest(t, body))
	b.WriteByte('\n')
	b.Write(canonicalBytes(t, struct {
		Owner  authority.Subject       `json:"owner"`
		Signer authority.CredentialRef `json:"signer"`
	}{Owner: owner, Signer: signer}))
	return []byte(b.String())
}

func signConsent(t *testing.T, body authority.ConsentBody, approvals ...struct {
	owner  authority.Subject
	signer authority.CredentialRef
	seed   byte
}) authority.SignedConsent {
	t.Helper()
	out := authority.SignedConsent{Body: body}
	for _, a := range approvals {
		sig := ed25519.Sign(fixedPrivateKey(a.seed), consentApprovalBytes(t, body, a.owner, a.signer))
		out.Approvals = append(out.Approvals, authority.ConsentApproval{
			Owner: a.owner, Signer: a.signer, Signature: base64.StdEncoding.EncodeToString(sig),
		})
	}
	return out
}

func signProof(t *testing.T, body authority.Proof, seed byte) ([]byte, string) {
	t.Helper()
	canon := canonicalBytes(t, body)
	h := sha256.New()
	h.Write([]byte("magi:canonical:v1:authority_proof\n"))
	h.Write(canon)
	digest := hex.EncodeToString(h.Sum(nil))
	id := "aprf_" + digest
	msg := append([]byte(proofSigningDomain), canon...)
	sig := ed25519.Sign(fixedPrivateKey(seed), msg)
	wrapper := authority.SignedProof{
		Proof: body, ProofID: id, Signature: base64.StdEncoding.EncodeToString(sig),
	}
	return canonicalBytes(t, wrapper), id
}

func proofFixture(t *testing.T) (authority.TrustState, authority.VerificationRequest, []byte, string) {
	t.Helper()
	governance := credentialFor(1, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope")
	owner := credentialFor(2, "cred-owner", "owner-a", "identity-source", "identity-v1", "credential-scope")
	scopes := []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	policy := authority.OwnerPolicy{
		RootIncarnation: "root-1",
		PolicyID:        "owner-policy", PolicyVersion: "1",
		Mode: authority.OwnerThreshold, Threshold: 1,
		Owners:     []authority.Subject{owner.Subject},
		Resources:  []string{"case/42"},
		Actions:    []string{"PUBLISH"},
		Tenants:    []string{"tenant-a"},
		Operations: []string{"PUBLISH_ARTIFACT"},
		NotBefore:  "2026-10-24T09:00:00.000Z",
		ExpiresAt:  "2026-10-24T12:00:00.000Z",
		Status:     authority.PolicyActive,
	}
	consentBody := authority.ConsentBody{
		Version:         authority.ConsentVersion,
		ConsentID:       "consent-1",
		TrustDomain:     "tenant-a",
		RootIncarnation: "root-1",
		PolicyID:        policy.PolicyID, PolicyVersion: policy.PolicyVersion,
		DelegatedSubject: governance.Subject,
		Resource:         "case/42", Action: "PUBLISH", Tenant: "tenant-a",
		Operation: "PUBLISH_ARTIFACT", CaseID: "case-42",
		Round: 2, Scopes: scopes,
		NotBefore: "2026-10-24T09:00:00.000Z", ExpiresAt: "2026-10-24T12:00:00.000Z",
	}
	consent := signConsent(t, consentBody, struct {
		owner  authority.Subject
		signer authority.CredentialRef
		seed   byte
	}{owner: owner.Subject, signer: owner.Ref, seed: 2})
	proof := authority.Proof{
		Version:     authority.ProofVersion,
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		Issuer: governance.Subject, Subject: governance.Subject,
		Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a",
		Operation: "PUBLISH_ARTIFACT", CaseID: "case-42",
		Round: 2, ExecutionGeneration: 7,
		Scopes:   scopes,
		IssuedAt: "2026-10-24T09:30:00.000Z", ExpiresAt: "2026-10-24T09:40:00.000Z",
		Signer:  governance.Ref,
		Consent: consent,
	}
	raw, id := signProof(t, proof, 1)
	now := "2026-10-24T09:35:00.000Z"
	req := authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: id,
		Subject: governance.Subject, Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a",
		Operation: "PUBLISH_ARTIFACT", CaseID: "case-42", Round: 2, ExecutionGeneration: 7,
		Scopes: scopes,
	}
	state := authority.TrustState{
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		RootStatus:            authority.RootActive,
		IdentitySourceID:      "identity-source",
		IdentitySourceVersion: "identity-v1",
		GovernanceSubject:     governance.Subject,
		RootGrant: authority.AuthorityGrant{
			Resources: []string{"case/42"},
			Actions:   []string{"PUBLISH"},
			Tenants:   []string{"tenant-a"},
		},
		TrustedNow:  now,
		Credentials: []authority.Credential{governance, owner},
		Policies:    []authority.OwnerPolicy{policy},
		Epochs: []authority.EpochState{
			{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
			{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
			{Scope: authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "credential-scope"}, Epoch: 5},
		},
	}
	return state, req, raw, id
}

func TestProofVerificationRealEd25519Vector(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	parsed, result := authority.ParseSignedProof(raw, id)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("parse outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	result = authority.VerifySignedProof(parsed, req, state)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("verify outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if !result.LocalAuthenticationOnly || result.CommitEligible || result.MutationAuthorized {
		t.Fatalf("VALID result must be local-auth-only and non-authorizing: %+v", result)
	}
}

func TestProofVerificationRejectsTamperedSignature(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	parsed, result := authority.ParseSignedProof(raw, id)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("parse outcome=%s code=%s", result.Outcome, result.Code)
	}
	sig, err := base64.StdEncoding.DecodeString(parsed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 1
	parsed.Signature = base64.StdEncoding.EncodeToString(sig)
	result = authority.VerifySignedProof(parsed, req, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_SIGNATURE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}
