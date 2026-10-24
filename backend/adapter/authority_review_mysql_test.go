package magi_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/authority"
)

func TestReviewMySQL_CredentialScopeClosure(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	actual := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "actual-credential-scope"}
	declared := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "declared-credential-scope"}
	bootstrap := f.base.bootstrap
	bootstrap.InitialScopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: actual.Kind, ScopeID: actual.ScopeID, Epoch: 7},
		{Kind: declared.Kind, ScopeID: declared.ScopeID, Epoch: 6},
	}
	for i := range bootstrap.Credentials {
		bootstrap.Credentials[i].Scope = actual
	}
	bootstrapRaw, trust := authoritySignedBootstrap(t, bootstrap, 9)
	repo := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: trust})
	if _, _, result := repo.BootstrapRoot(ctx, bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	proof := f.base.proof
	proof.Scopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: declared.Kind, ScopeID: declared.ScopeID, Epoch: 6},
	}
	alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
	raw, id := authoritySignedProof(t, proof, 1)
	result := repo.RegisterProof(ctx, raw, id)
	if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_SCOPE_CLOSURE_MISMATCH" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	assertAuthorityProofNotPersisted(t, f, id)
}

func TestReviewMySQL_RootStatusFailsClosed(t *testing.T) {
	cases := []struct {
		status  string
		outcome authority.Outcome
		code    string
	}{
		{"RETIRED", authority.OutcomeRevoked, "TRUST_ROOT_RETIRED"},
		{"REVOKED", authority.OutcomeRevoked, "TRUST_ROOT_REVOKED"},
		{"FUTURE", authority.OutcomeUnknown, "TRUST_ROOT_STATUS_UNKNOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			f := newAuthorityMySQL(t)
			ctx := context.Background()
			if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
				t.Fatal(result)
			}
			if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
				t.Fatal(result)
			}
			if err := f.db.Exec(`UPDATE magi_authority_root SET status = ? WHERE trust_domain = ? AND active_slot = 1`,
				tc.status, "tenant-a").Error; err != nil {
				t.Fatal(err)
			}
			result := f.repo.VerifyProof(ctx, f.base.request)
			if result.Outcome != tc.outcome || result.Code != tc.code {
				t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
			}
		})
	}
}

func TestReviewMySQL_CredentialRootIncarnationPinned(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	old := f.base.governance
	old.Ref.RootIncarnation = "root-old"
	rawCredential := authorityCanonical(t, old)
	publicKey, err := base64.StdEncoding.DecodeString(old.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	now := f.base.now
	if err := f.db.Exec(`INSERT INTO magi_authority_credential
		(trust_domain, root_incarnation, credential_id, credential_version,
		 subject_id, identity_source_id, identity_source_version, algorithm,
		 public_key, not_before, expires_at, status, credential_scope_id,
		 created_at, raw_credential)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"tenant-a", old.Ref.RootIncarnation, old.Ref.CredentialID, old.Ref.CredentialVersion,
		old.Subject.SubjectID, old.Subject.IdentitySourceID, old.Subject.IdentitySourceVersion,
		old.Algorithm, publicKey, now.Add(-time.Hour), now.Add(2*time.Hour), old.Status,
		old.Scope.ScopeID, now, rawCredential).Error; err != nil {
		t.Fatal(err)
	}
	proof := f.base.proof
	proof.Signer = old.Ref
	raw, id := authoritySignedProof(t, proof, 1)
	result := f.repo.RegisterProof(ctx, raw, id)
	if result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROOT_MISMATCH" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	assertAuthorityProofNotPersisted(t, f, id)
}

func TestReviewMySQL_CommandReplyLossAndConflict(t *testing.T) {
	t.Run("revocation", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
		command := authorityRevocationCommand(t, f, "revoke-replay", scope, 4, 1, "first")
		first, result := f.repo.RevokeScope(ctx, command)
		if result.Outcome != authority.OutcomeValid {
			t.Fatalf("first outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		retry, result := f.repo.RevokeScope(ctx, command)
		if result.Outcome != authority.OutcomeValid || result.Code != "REVOCATION_COMMAND_REPLAY" ||
			retry.Epoch != first.Epoch || retry.RowVersion != first.RowVersion {
			t.Fatalf("retry epoch=%+v result=%+v", retry, result)
		}
		different := authorityRevocationCommand(t, f, "revoke-replay", scope, 4, 1, "different bytes")
		if _, result := f.repo.RevokeScope(ctx, different); result.Outcome != authority.OutcomeConflict || result.Code != "COMMAND_ID_CONFLICT" {
			t.Fatalf("conflict outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("credential rotation", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope",
			f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
		command := authorityRotationCommand(t, f, "rotate-replay", f.base.governance.Ref, replacement, 5, 1, "first")
		first, result := f.repo.RotateCredential(ctx, command)
		if result.Outcome != authority.OutcomeValid {
			t.Fatalf("first outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		retry, result := f.repo.RotateCredential(ctx, command)
		if result.Outcome != authority.OutcomeValid || result.Code != "CREDENTIAL_ROTATION_COMMAND_REPLAY" ||
			retry.Ref != first.Ref || retry.PublicKey != first.PublicKey {
			t.Fatalf("retry credential=%+v result=%+v", retry, result)
		}
		different := authorityRotationCommand(t, f, "rotate-replay", f.base.governance.Ref, replacement, 5, 1, "different bytes")
		if _, result := f.repo.RotateCredential(ctx, different); result.Outcome != authority.OutcomeConflict || result.Code != "COMMAND_ID_CONFLICT" {
			t.Fatalf("conflict outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestReviewMySQL_ScopeEvidenceExactBytes(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	nonCanonical := []byte(`{ "kind": "CONTEXT", "scope_id": "context-scope", "epoch": 4 }`)
	if err := f.db.Exec(`UPDATE magi_authority_proof_scope SET raw_scope = ?
		WHERE trust_domain = ? AND proof_id = ? AND scope_kind = 'CONTEXT'`,
		nonCanonical, "tenant-a", f.base.proofID).Error; err != nil {
		t.Fatal(err)
	}
	result := f.repo.VerifyProof(ctx, f.base.request)
	if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_SCOPE_EVIDENCE_CORRUPT" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestReviewMySQL_ScopeUTF16RepositoryClosure(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	nonBMP := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "\U00010000"}
	bmp := authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "\uE000"}
	bootstrap := f.base.bootstrap
	bootstrap.Credentials = append([]authority.Credential(nil), f.base.bootstrap.Credentials...)
	for i := range bootstrap.Credentials {
		bootstrap.Credentials[i].Scope = nonBMP
	}
	bootstrap.Credentials[1].Scope = bmp
	bootstrap.InitialScopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: nonBMP.Kind, ScopeID: nonBMP.ScopeID, Epoch: 5},
		{Kind: bmp.Kind, ScopeID: bmp.ScopeID, Epoch: 6},
	}
	bootstrapRaw, trust := authoritySignedBootstrap(t, bootstrap, 9)
	repo := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: trust})
	if _, _, result := repo.BootstrapRoot(ctx, bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	proof := f.base.proof
	proof.Scopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: nonBMP.Kind, ScopeID: nonBMP.ScopeID, Epoch: 5},
		{Kind: bmp.Kind, ScopeID: bmp.ScopeID, Epoch: 6},
	}
	alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
	raw, id := authoritySignedProof(t, proof, 1)
	if result := repo.RegisterProof(ctx, raw, id); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	req := f.base.request
	req.ProofID = id
	req.Scopes = append([]authority.ScopedEpoch(nil), proof.Scopes...)
	if result := repo.VerifyProof(ctx, req); result.Outcome != authority.OutcomeValid {
		t.Fatalf("verify outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestReviewMySQL_OversizedAuthenticatedStructures(t *testing.T) {
	t.Run("delegations", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		proof := f.base.proof
		proof.Delegations = make([]authority.SignedDelegation, 17)
		raw, id := authoritySignedProof(t, proof, 1)
		result := f.repo.RegisterProof(ctx, raw, id)
		if result.Outcome != authority.OutcomeDenied || result.Code != "DELEGATION_LIMIT_EXCEEDED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		assertAuthorityProofNotPersisted(t, f, id)
	})

	t.Run("approvals", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		proof := f.base.proof
		approval := proof.Consent.Approvals[0]
		proof.Consent.Approvals = make([]authority.ConsentApproval, 65)
		for i := range proof.Consent.Approvals {
			proof.Consent.Approvals[i] = approval
		}
		raw, id := authoritySignedProof(t, proof, 1)
		result := f.repo.RegisterProof(ctx, raw, id)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_LIMIT_EXCEEDED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		assertAuthorityProofNotPersisted(t, f, id)
	})
}

func TestReviewMySQL_BootstrapCredentialScopeKindRejected(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	bootstrap := f.base.bootstrap
	bootstrap.Credentials = append([]authority.Credential(nil), f.base.bootstrap.Credentials...)
	bootstrap.Credentials[0].Scope.Kind = authority.ScopeAuthority
	bootstrap.InitialScopes = []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeAuthority, ScopeID: "credential-scope", Epoch: 5},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	raw, trust := authoritySignedBootstrap(t, bootstrap, 9)
	repo := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: trust})
	if _, _, result := repo.BootstrapRoot(ctx, raw); result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_CREDENTIAL_SCOPE_INVALID" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	var roots int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM magi_authority_root`).Scan(&roots).Error; err != nil || roots != 0 {
		t.Fatalf("roots=%d err=%v", roots, err)
	}
}
