package authority_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
)

func resealDirectProof(t *testing.T, state authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
	t.Helper()
	owner := state.Credentials[1]
	proof.Proof.Consent.Body.DelegatedSubject = proof.Proof.Subject
	proof.Proof.Consent.Body.Resource = proof.Proof.Resource
	proof.Proof.Consent.Body.Action = proof.Proof.Action
	proof.Proof.Consent.Body.Tenant = proof.Proof.Tenant
	proof.Proof.Consent.Body.Operation = proof.Proof.Operation
	proof.Proof.Consent.Body.CaseID = proof.Proof.CaseID
	proof.Proof.Consent.Body.Round = proof.Proof.Round
	proof.Proof.Consent.Body.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
	proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
		owner  authority.Subject
		signer authority.CredentialRef
		seed   byte
	}{owner.Subject, owner.Ref, 2})
	reSignFixture(t, proof, req, 1)
}

func TestReviewCredentialScopeClosure(t *testing.T) {
	t.Run("declared credential scope omits signer and owner scopes", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		actual := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "actual-credential-scope"}
		declared := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "declared-credential-scope"}
		state.Credentials[0].Scope = actual
		state.Credentials[1].Scope = actual
		proof.Proof.Scopes = []authority.ScopedEpoch{
			{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
			{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
			{Kind: declared.Kind, ScopeID: declared.ScopeID, Epoch: 6},
		}
		state.Epochs = []authority.EpochState{
			{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
			{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
			{Scope: actual, Epoch: 7},
			{Scope: declared, Epoch: 6},
		}
		req.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_CLOSURE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner approval credential scope is omitted", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		signerScope := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "signer-scope"}
		ownerScope := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "owner-scope"}
		state.Credentials[0].Scope = signerScope
		state.Credentials[1].Scope = ownerScope
		proof.Proof.Scopes = []authority.ScopedEpoch{
			{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
			{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
			{Kind: signerScope.Kind, ScopeID: signerScope.ScopeID, Epoch: 5},
		}
		state.Epochs = []authority.EpochState{
			{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
			{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
			{Scope: ownerScope, Epoch: 6},
			{Scope: signerScope, Epoch: 5},
		}
		req.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_CLOSURE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("delegation issuer credential scope is omitted", func(t *testing.T) {
		state, req, raw, id := delegationFixture(t)
		proof := mustParseProof(t, raw, id)
		governanceScope := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "governance-scope"}
		delegateScope := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "delegate-scope"}
		ownerScope := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "owner-scope"}
		state.Credentials[0].Scope = governanceScope
		state.Credentials[1].Scope = ownerScope
		state.Credentials[2].Scope = delegateScope
		proof.Proof.Scopes = []authority.ScopedEpoch{
			{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
			{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
			{Kind: delegateScope.Kind, ScopeID: delegateScope.ScopeID, Epoch: 5},
			{Kind: ownerScope.Kind, ScopeID: ownerScope.ScopeID, Epoch: 6},
		}
		state.Epochs = []authority.EpochState{
			{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
			{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
			{Scope: governanceScope, Epoch: 7},
			{Scope: delegateScope, Epoch: 5},
			{Scope: ownerScope, Epoch: 6},
		}
		req.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
		owner := state.Credentials[1]
		proof.Proof.Consent.Body.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 3)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_CLOSURE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("credential scope kind must be CREDENTIAL", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Credentials[1].Scope.Kind = authority.ScopeAuthority
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_KIND_INVALID" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("extra credential scope is not in the used closure", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		used := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "used-scope"}
		extra := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "extra-scope"}
		state.Credentials[0].Scope = used
		state.Credentials[1].Scope = used
		proof.Proof.Scopes = []authority.ScopedEpoch{
			{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
			{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
			{Kind: extra.Kind, ScopeID: extra.ScopeID, Epoch: 6},
			{Kind: used.Kind, ScopeID: used.ScopeID, Epoch: 5},
		}
		state.Epochs = []authority.EpochState{
			{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
			{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
			{Scope: extra, Epoch: 6},
			{Scope: used, Epoch: 5},
		}
		req.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_CLOSURE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("ambiguous credential ref", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Credentials = append(state.Credentials, state.Credentials[0])
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeConflict || result.Code != "CREDENTIAL_AMBIGUOUS" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("stale used credential scope", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Epochs[2].Epoch = 6
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeStale || result.Code != "SCOPED_EPOCH_STALE" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("revoked used credential scope", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Epochs[2].Revoked = true
		state.Epochs[2].Epoch = 6
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeRevoked || result.Code != "SCOPE_REVOKED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewRootGrantContainment(t *testing.T) {
	t.Run("zero-hop proof outside root grant", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Resource = "case/99"
		req.Resource = "case/99"
		state.Policies[0].Resources = []string{"case/99"}
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_OUTSIDE_ROOT_GRANT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner policy outside root grant", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		c, err := canonical.Canonicalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		var envelope authority.BootstrapEnvelope
		if err := json.Unmarshal(c.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Bootstrap.OwnerPolicies[0].Resources = []string{"case/99"}
		raw, trust = signBootstrap(t, envelope.Bootstrap, 9)
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_OWNER_POLICY_OUTSIDE_ROOT_GRANT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewCredentialRootIncarnationPinned(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	proof := mustParseProof(t, raw, id)
	oldRoot := credentialFor(1, "cred-governance-old-root", "governance", "identity-source", "identity-v1", "credential-scope")
	oldRoot.Ref.RootIncarnation = "root-0"
	oldRoot.PublicKey = state.Credentials[0].PublicKey
	state.Credentials = append(state.Credentials, oldRoot)
	proof.Proof.Signer = oldRoot.Ref
	reSignFixture(t, &proof, &req, 1)
	result := authority.VerifySignedProof(proof, req, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROOT_MISMATCH" {
		t.Fatalf("signer outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}

	t.Run("owner approval", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		oldOwner := state.Credentials[1]
		oldOwner.Ref.RootIncarnation = "root-0"
		state.Credentials = append(state.Credentials, oldOwner)
		approval := proof.Proof.Consent.Approvals[0]
		approval.Signer = oldOwner.Ref
		approval.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(fixedPrivateKey(2), consentApprovalBytes(t, proof.Proof.Consent.Body, approval.Owner, approval.Signer)))
		proof.Proof.Consent.Approvals[0] = approval
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROOT_MISMATCH" {
			t.Fatalf("owner outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("delegation issuer", func(t *testing.T) {
		state, req, raw, id := delegationFixture(t)
		proof := mustParseProof(t, raw, id)
		oldGovernance := state.Credentials[0]
		oldGovernance.Ref.RootIncarnation = "root-0"
		state.Credentials = append(state.Credentials, oldGovernance)
		hop := proof.Proof.Delegations[0]
		hop.Body.IssuerCredential = oldGovernance.Ref
		proof.Proof.Delegations[0] = signDelegation(t, hop.Body, 1)
		reSignFixture(t, &proof, &req, 3)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROOT_MISMATCH" {
			t.Fatalf("delegation outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewScopeUTF16Ordering(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	proof := mustParseProof(t, raw, id)
	nonBMP := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "\U00010000"}
	bmp := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "\uE000"}
	state.Credentials[0].Scope = nonBMP
	state.Credentials[1].Scope = bmp
	// UTF-16 code-unit order is D800 DC00 < E000. UTF-8 byte order would
	// incorrectly place U+E000 first.
	proof.Proof.Scopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: nonBMP.Kind, ScopeID: nonBMP.ScopeID, Epoch: 5},
		{Kind: bmp.Kind, ScopeID: bmp.ScopeID, Epoch: 6},
	}
	state.Epochs = []authority.EpochState{
		{Scope: authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, Epoch: 3},
		{Scope: authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, Epoch: 4},
		{Scope: nonBMP, Epoch: 5},
		{Scope: bmp, Epoch: 6},
	}
	req.Scopes = append([]authority.ScopedEpoch(nil), proof.Proof.Scopes...)
	resealDirectProof(t, state, &proof, &req)
	result := authority.VerifySignedProof(proof, req, state)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestReviewAuthenticatedStructureLimits(t *testing.T) {
	t.Run("delegation hops", func(t *testing.T) {
		state, req, raw, id := delegationFixture(t)
		proof := mustParseProof(t, raw, id)
		hop := proof.Proof.Delegations[0]
		proof.Proof.Delegations = make([]authority.SignedDelegation, 17)
		for i := range proof.Proof.Delegations {
			proof.Proof.Delegations[i] = hop
		}
		reSignFixture(t, &proof, &req, 3)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "DELEGATION_LIMIT_EXCEEDED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner approvals", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		approval := proof.Proof.Consent.Approvals[0]
		proof.Proof.Consent.Approvals = make([]authority.ConsentApproval, 65)
		for i := range proof.Proof.Consent.Approvals {
			proof.Proof.Consent.Approvals[i] = approval
		}
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_LIMIT_EXCEEDED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner policy owners", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		owner := state.Policies[0].Owners[0]
		state.Policies[0].Owners = make([]authority.Subject, 65)
		for i := range state.Policies[0].Owners {
			state.Policies[0].Owners[i] = owner
		}
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_POLICY_LIMIT_EXCEEDED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewBootstrapCredentialScopeMustBeCredential(t *testing.T) {
	raw, trust := bootstrapFixture(t)
	c, err := canonical.Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var envelope authority.BootstrapEnvelope
	if err := json.Unmarshal(c.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Bootstrap.Credentials[0].Scope.Kind = authority.ScopeAuthority
	envelope.Bootstrap.InitialScopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeAuthority, ScopeID: "credential-scope", Epoch: 5},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	raw, trust = signBootstrap(t, envelope.Bootstrap, 9)
	_, _, result := authority.AuthenticateBootstrap(raw, trust)
	if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_CREDENTIAL_SCOPE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestReviewConsentExecutionGenerationBinding(t *testing.T) {
	t.Run("substitution", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Consent.Body.ExecutionGeneration = 8
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_GENERATION_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("generation zero is historical", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.ExecutionGeneration = 0
		proof.Proof.Consent.Body.ExecutionGeneration = 0
		req.ExecutionGeneration = 0
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "EXECUTION_GENERATION_INVALID" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewRootAndDelegationOperationContainment(t *testing.T) {
	t.Run("zero-hop operation outside root grant", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Operation = "SEND_COMMIT"
		req.Operation = "SEND_COMMIT"
		state.Policies[0].Operations = []string{"SEND_COMMIT"}
		resealDirectProof(t, state, &proof, &req)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_OUTSIDE_ROOT_GRANT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("delegation operation widening", func(t *testing.T) {
		state, req, raw, id := delegationFixture(t)
		proof := mustParseProof(t, raw, id)
		hop := proof.Proof.Delegations[0]
		hop.Body.Operations = []string{"SEND_COMMIT"}
		proof.Proof.Delegations[0] = signDelegation(t, hop.Body, 1)
		reSignFixture(t, &proof, &req, 3)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "DELEGATION_SCOPE_WIDENED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("proof operation outside final delegation", func(t *testing.T) {
		state, req, raw, id := delegationFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Operation = "SEND_COMMIT"
		req.Operation = "SEND_COMMIT"
		state.RootGrant.Operations = []string{"PUBLISH_ARTIFACT", "SEND_COMMIT"}
		state.Policies[0].Operations = []string{"SEND_COMMIT"}
		proof.Proof.Consent.Body.Operation = "SEND_COMMIT"
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 3)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_OUTSIDE_DELEGATION" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("owner policy operation outside root grant", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		c, err := canonical.Canonicalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		var envelope authority.BootstrapEnvelope
		if err := json.Unmarshal(c.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Bootstrap.OwnerPolicies[0].Operations = []string{"SEND_COMMIT"}
		raw, trust = signBootstrap(t, envelope.Bootstrap, 9)
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_OWNER_POLICY_OUTSIDE_ROOT_GRANT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewBootstrapOwnerLimitRejectedBeforeOwnerExpansion(t *testing.T) {
	raw, trust := bootstrapFixture(t)
	c, err := canonical.Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var envelope authority.BootstrapEnvelope
	if err := json.Unmarshal(c.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	owner := envelope.Bootstrap.OwnerPolicies[0].Owners[0]
	envelope.Bootstrap.OwnerPolicies[0].Owners = make([]authority.Subject, 65)
	for i := range envelope.Bootstrap.OwnerPolicies[0].Owners {
		envelope.Bootstrap.OwnerPolicies[0].Owners[i] = owner
	}
	raw, trust = signBootstrap(t, envelope.Bootstrap, 9)
	_, _, result := authority.AuthenticateBootstrap(raw, trust)
	if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_OWNER_POLICY_LIMIT_EXCEEDED" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestReviewScopeIDsRequireNFC(t *testing.T) {
	state, req, raw, id := proofFixture(t)
	proof := mustParseProof(t, raw, id)
	req.Scopes = append([]authority.ScopedEpoch(nil), req.Scopes...)
	req.Scopes[1].ScopeID = "context-scope-e\u0301"
	result := authority.VerifySignedProof(proof, req, state)
	if result.Outcome != authority.OutcomeDenied || result.Code != "REQUEST_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}
