package magi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosqlmysql "github.com/go-sql-driver/mysql"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var authoritySchemaSeq atomic.Int64

type authoritySQLFixture struct {
	db      *gorm.DB
	repo    *magi.AuthorityRepository
	base    c2SignedFixture
	trust   authority.BootstrapTrust
	rootFPs string
}

type c2SignedFixture struct {
	now          time.Time
	bootstrap    authority.TrustBootstrap
	bootstrapRaw []byte
	trust        authority.BootstrapTrust
	proof        authority.Proof
	proofRaw     []byte
	proofID      string
	request      authority.VerificationRequest
	governance   authority.Credential
	ownerA       authority.Credential
	ownerB       authority.Credential
	delegateA    authority.Credential
	delegateB    authority.Credential
	scopes       []authority.ScopedEpoch
	policy       authority.OwnerPolicy
}

func authoritySeed(seed byte) ed25519.PrivateKey {
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed
	}
	return ed25519.NewKeyFromSeed(raw)
}

func authorityCredential(seed byte, version uint64, id, subject, scopeID string, notBefore, expiresAt time.Time) authority.Credential {
	pub := authoritySeed(seed).Public().(ed25519.PublicKey)
	return authority.Credential{
		Ref: authority.CredentialRef{RootIncarnation: "root-c2", CredentialID: id, CredentialVersion: version},
		Subject: authority.Subject{
			SubjectID: subject, IdentitySourceID: "identity-source", IdentitySourceVersion: "identity-v1",
		},
		Algorithm: authority.SignatureAlgorithmEd25519,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		NotBefore: authorityTime(notBefore), ExpiresAt: authorityTime(expiresAt),
		Status: authority.CredentialActive,
		Scope:  authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: scopeID},
	}
}

func authorityTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func authorityCanonical(t *testing.T, v any) []byte {
	t.Helper()
	c, err := canonical.CanonicalizeStruct(v)
	if err != nil {
		t.Fatalf("canonicalize authority fixture: %v", err)
	}
	return c.Bytes()
}

func authoritySign(t *testing.T, domain string, v any, seed byte) string {
	t.Helper()
	msg := append([]byte(domain), authorityCanonical(t, v)...)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(authoritySeed(seed), msg))
}

func authorityProofID(t *testing.T, body authority.Proof) (string, string) {
	t.Helper()
	c, err := canonical.CanonicalizeStruct(body)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := c.Digest(canonical.TypeAuthorityProof)
	if err != nil {
		t.Fatal(err)
	}
	return "aprf_" + digest, digest
}

func authorityConsentDigest(t *testing.T, body authority.ConsentBody) string {
	t.Helper()
	h := sha256.New()
	h.Write([]byte("magi:authority-consent-evidence:v1\n"))
	h.Write(authorityCanonical(t, body))
	return hex.EncodeToString(h.Sum(nil))
}

func authorityConsent(t *testing.T, body authority.ConsentBody, approvals ...authority.ConsentApproval) authority.SignedConsent {
	t.Helper()
	out := authority.SignedConsent{Body: body, Approvals: approvals}
	return out
}

func authorityApproval(t *testing.T, body authority.ConsentBody, owner authority.Credential, seed byte) authority.ConsentApproval {
	t.Helper()
	binding := authorityCanonical(t, struct {
		Owner  authority.Subject       `json:"owner"`
		Signer authority.CredentialRef `json:"signer"`
	}{owner.Subject, owner.Ref})
	msg := []byte("magi:authority-consent-approval:v1\n" + authorityConsentDigest(t, body) + "\n")
	msg = append(msg, binding...)
	return authority.ConsentApproval{
		Owner: owner.Subject, Signer: owner.Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(authoritySeed(seed), msg)),
	}
}

func authoritySignedProof(t *testing.T, body authority.Proof, seed byte) ([]byte, string) {
	t.Helper()
	id, _ := authorityProofID(t, body)
	msg := append([]byte("magi:authority-proof-signature:v1\n"), authorityCanonical(t, body)...)
	sig := ed25519.Sign(authoritySeed(seed), msg)
	wrapper := authority.SignedProof{Proof: body, ProofID: id, Signature: base64.StdEncoding.EncodeToString(sig)}
	return authorityCanonical(t, wrapper), id
}

func authoritySignedBootstrap(t *testing.T, body authority.TrustBootstrap, seed byte) ([]byte, authority.BootstrapTrust) {
	t.Helper()
	msg := append([]byte("magi:authority-bootstrap-signature:v1\n"), authorityCanonical(t, body)...)
	sig := ed25519.Sign(authoritySeed(seed), msg)
	env := authority.BootstrapEnvelope{
		Bootstrap: body, KeyID: "recovery-key", KeyVersion: 1,
		Algorithm: authority.SignatureAlgorithmEd25519,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	return authorityCanonical(t, env), authority.BootstrapTrust{
		IdentitySourceID: "identity-source", IdentitySourceVersion: "identity-v1",
		RecoveryKeyID: "recovery-key", RecoveryKeyVersion: 1,
		Algorithm: authority.SignatureAlgorithmEd25519,
		PublicKey: base64.StdEncoding.EncodeToString(authoritySeed(seed).Public().(ed25519.PublicKey)),
	}
}

func newC2SignedFixture(t *testing.T) c2SignedFixture {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	notBefore, expiresAt := now.Add(-time.Hour), now.Add(2*time.Hour)
	governance := authorityCredential(1, 1, "cred-governance", "governance", "credential-scope", notBefore, expiresAt)
	ownerA := authorityCredential(2, 1, "cred-owner-a", "owner-a", "credential-scope", notBefore, expiresAt)
	ownerB := authorityCredential(7, 1, "cred-owner-b", "owner-b", "credential-scope", notBefore, expiresAt)
	delegateA := authorityCredential(3, 1, "cred-delegate-a", "delegate-a", "credential-scope", notBefore, expiresAt)
	delegateB := authorityCredential(4, 1, "cred-delegate-b", "delegate-b", "credential-scope", notBefore, expiresAt)
	scopes := []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
		{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	policy := authority.OwnerPolicy{
		RootIncarnation: "root-c2",
		PolicyID:        "owner-policy", PolicyVersion: "1", Mode: authority.OwnerThreshold, Threshold: 1,
		Owners: []authority.Subject{ownerA.Subject}, Resources: []string{"case/42"},
		Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"}, Operations: []string{"PUBLISH_ARTIFACT"},
		NotBefore: authorityTime(notBefore), ExpiresAt: authorityTime(expiresAt), Status: authority.PolicyActive,
	}
	consentBody := authority.ConsentBody{
		Version: authority.ConsentVersion, ConsentID: "consent-c2",
		TrustDomain: "tenant-a", RootIncarnation: "root-c2",
		PolicyID: policy.PolicyID, PolicyVersion: policy.PolicyVersion,
		DelegatedSubject: governance.Subject,
		Resource:         "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, ExecutionGeneration: 7, Scopes: scopes,
		NotBefore: authorityTime(now.Add(-5 * time.Minute)), ExpiresAt: authorityTime(expiresAt),
	}
	proof := authority.Proof{
		Version: authority.ProofVersion, TrustDomain: "tenant-a", RootIncarnation: "root-c2",
		Issuer: governance.Subject, Subject: governance.Subject,
		Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, ExecutionGeneration: 7, Scopes: scopes,
		IssuedAt: authorityTime(now.Add(-5 * time.Minute)), ExpiresAt: authorityTime(now.Add(30 * time.Minute)),
		Signer:  governance.Ref,
		Consent: authorityConsent(t, consentBody, authorityApproval(t, consentBody, ownerA, 2)),
	}
	raw, id := authoritySignedProof(t, proof, 1)
	bootstrap := authority.TrustBootstrap{
		Version: authority.BootstrapVersion, TrustDomain: "tenant-a", RootIncarnation: "root-c2",
		IdentitySourceID: "identity-source", IdentitySourceVersion: "identity-v1",
		GovernanceSubject: governance.Subject,
		RootGrant: authority.AuthorityGrant{
			Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"},
			Operations: []string{"PUBLISH_ARTIFACT"},
		},
		Credentials:   []authority.Credential{governance, ownerA, ownerB, delegateA, delegateB},
		OwnerPolicies: []authority.OwnerPolicy{policy},
		InitialScopes: scopes,
	}
	bootstrapRaw, trust := authoritySignedBootstrap(t, bootstrap, 9)
	req := authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: id, Subject: governance.Subject,
		Resource: "case/42", Action: "PUBLISH", Tenant: "tenant-a", Operation: "PUBLISH_ARTIFACT",
		CaseID: "case-42", Round: 2, ExecutionGeneration: 7, Scopes: scopes,
	}
	return c2SignedFixture{
		now: now, bootstrap: bootstrap, bootstrapRaw: bootstrapRaw, trust: trust,
		proof: proof, proofRaw: raw, proofID: id, request: req,
		governance: governance, ownerA: ownerA, ownerB: ownerB, delegateA: delegateA, delegateB: delegateB,
		scopes: scopes, policy: policy,
	}
}

func applyAuthorityMigrationFile(t *testing.T, db *gorm.DB) {
	t.Helper()
	migration, err := os.ReadFile("../../docker/atlas/migrations/magi_s32_authority_proof.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, stmt := range strings.Split(string(migration), ";") {
		lines := strings.Split(stmt, "\n")
		kept := lines[:0]
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			kept = append(kept, line)
		}
		stmt = strings.TrimSpace(strings.Join(kept, "\n"))
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("apply authority migration: %v\n%s", err, stmt)
		}
	}
}

func newAuthorityMySQL(t *testing.T) *authoritySQLFixture {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("MAGI_TEST_MYSQL_DSN"))
	if base == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration tests — a green run without it does NOT mean MySQL was verified")
	}
	cfg, err := gosqlmysql.ParseDSN(base)
	if err != nil {
		t.Fatalf("parse MAGI_TEST_MYSQL_DSN: %v", err)
	}
	adminCfg := *cfg
	adminCfg.DBName = ""
	admin, err := gorm.Open(mysql.Open(adminCfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	name := fmt.Sprintf("magi_c2_%d_%d", os.Getpid(), authoritySchemaSeq.Add(1))
	if err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatalf("create authority schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error
		if sqlDB, dbErr := admin.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open authority schema: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(24)
	t.Cleanup(func() { _ = sqlDB.Close() })
	applyAuthorityMigrationFile(t, db)
	baseFixture := newC2SignedFixture(t)
	repo := magi.NewAuthorityRepository(db, magi.AuthorityRepositoryConfig{
		Enabled: true, RecoveryTrust: baseFixture.trust,
	})
	return &authoritySQLFixture{db: db, repo: repo, base: baseFixture, trust: baseFixture.trust}
}

func TestAuthorityMySQL_BootstrapProofRestartAndReplyLoss(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeUnknown || result.Code != "TRUST_ROOT_MISSING" {
		t.Fatalf("missing root outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	root, fingerprint, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw)
	if result.Outcome != authority.OutcomeValid || len(fingerprint) != 64 || root.RootIncarnation != "root-c2" {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s fp=%s", result.Outcome, result.Code, result.Detail, fingerprint)
	}
	if _, secondFingerprint, second := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); second.Outcome != authority.OutcomeValid || secondFingerprint != fingerprint || second.Code != "TRUST_ROOT_ALREADY_INSTALLED" {
		t.Fatalf("idempotent bootstrap outcome=%s code=%s detail=%s fp=%s", second.Outcome, second.Code, second.Detail, secondFingerprint)
	}
	other := f.base.bootstrap
	other.RootIncarnation = "root-other"
	for i := range other.Credentials {
		other.Credentials[i].Ref.RootIncarnation = "root-other"
	}
	other.OwnerPolicies[0].RootIncarnation = "root-other"
	otherRaw, _ := authoritySignedBootstrap(t, other, 9)
	if _, conflictFingerprint, conflict := f.repo.BootstrapRoot(ctx, otherRaw); conflict.Outcome != authority.OutcomeConflict || conflictFingerprint != "" {
		t.Fatalf("conflicting bootstrap outcome=%s code=%s detail=%s fp=%s", conflict.Outcome, conflict.Code, conflict.Detail, conflictFingerprint)
	}
	rootRaw, rootReadFingerprint, result := f.repo.ReadRoot(ctx, "tenant-a")
	if result.Outcome != authority.OutcomeValid || rootReadFingerprint != fingerprint || string(rootRaw) != string(f.base.bootstrapRaw) {
		t.Fatalf("root readback outcome=%s code=%s fp_equal=%v raw_equal=%v", result.Outcome, result.Code, rootReadFingerprint == fingerprint, string(rootRaw) == string(f.base.bootstrapRaw))
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	raw, result := f.repo.ReadProof(ctx, "tenant-a", f.base.proofID)
	if result.Outcome != authority.OutcomeValid || string(raw) != string(f.base.proofRaw) {
		t.Fatalf("readback outcome=%s code=%s raw_equal=%v", result.Outcome, result.Code, string(raw) == string(f.base.proofRaw))
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeValid || result.CommitEligible || result.MutationAuthorized {
		t.Fatalf("verify outcome=%s code=%s detail=%s result=%+v", result.Outcome, result.Code, result.Detail, result)
	}
	// A new repository instance proves state comes from MySQL, not an in-memory cache.
	restarted := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: f.trust})
	if result := restarted.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeValid {
		t.Fatalf("restart outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	// A committed registration with a lost reply is retried by proving and then
	// reading the same original row, not by inserting a second proof.
	if result := restarted.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("reply-loss retry outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	var count int64
	if err := f.db.Raw("SELECT COUNT(*) FROM magi_authority_proof WHERE trust_domain = ? AND proof_id = ?", "tenant-a", f.base.proofID).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("proof row count=%d err=%v", count, err)
	}
}

func TestAuthorityMySQL_RevocationMonotonicAndABA(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	initial, result := f.repo.ReadEpoch(ctx, "tenant-a", scope)
	if result.Outcome != authority.OutcomeValid || initial.Epoch != 4 || initial.RowVersion != 1 {
		t.Fatalf("initial epoch=%+v result=%+v", initial, result)
	}
	revoked, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "revoke-context-1", scope, 4, 1, "owner withdrawal"))
	if result.Outcome != authority.OutcomeValid || !revoked.Revoked || revoked.Epoch != 5 || revoked.RowVersion != 2 {
		t.Fatalf("revoke epoch=%+v result=%+v", revoked, result)
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeRevoked || result.Code != "SCOPE_REVOKED" {
		t.Fatalf("post-revoke outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if _, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "revoke-context-aba", scope, 3, 1, "replay old ABA")); result.Outcome != authority.OutcomeStale {
		t.Fatalf("stale revoke outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if _, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "revoke-context-duplicate", scope, 5, 2, "duplicate revoke")); result.Outcome != authority.OutcomeConflict {
		t.Fatalf("duplicate revoke outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	persisted, result := f.repo.ReadEpoch(ctx, "tenant-a", scope)
	if result.Outcome != authority.OutcomeValid || persisted.Epoch != 5 || !persisted.Revoked {
		t.Fatalf("persisted epoch=%+v result=%+v", persisted, result)
	}
	// CONTEXT, AUTHORITY, and CREDENTIAL use independent durable rows and
	// monotonically advance only the exact selected kind.
	for _, tc := range []struct {
		scope authority.ScopeKey
		epoch uint64
	}{
		{authority.ScopeKey{Kind: authority.ScopeAuthority, ScopeID: "authority-scope"}, 3},
		{authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "credential-scope"}, 5},
	} {
		revokedKind, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "revoke-"+string(tc.scope.Kind), tc.scope, tc.epoch, 1, "scoped withdrawal"))
		if result.Outcome != authority.OutcomeValid || !revokedKind.Revoked || revokedKind.Epoch != tc.epoch+1 {
			t.Fatalf("revoke %s epoch=%+v result=%+v", tc.scope.Kind, revokedKind, result)
		}
	}
	restarted := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: f.trust})
	if result := restarted.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeRevoked {
		t.Fatalf("restart revoked outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := restarted.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeRevoked {
		t.Fatalf("revoked proof replay outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestAuthorityMySQL_KeyRotationRejectsOldVersionAndRestart(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope",
		f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	newCredential, result := f.repo.RotateCredential(ctx, authorityRotationCommand(t, f, "rotate-governance-1", f.base.governance.Ref, replacement, 5, 1, "scheduled rotation"))
	if result.Outcome != authority.OutcomeValid || newCredential.Ref.CredentialVersion != 2 {
		t.Fatalf("rotate credential=%+v result=%+v", newCredential, result)
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeStale || result.Code != "CREDENTIAL_RETIRED" {
		t.Fatalf("old proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeStale || result.Code != "CREDENTIAL_RETIRED" {
		t.Fatalf("old proof replay outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	newProof := f.base.proof
	newProof.Signer = newCredential.Ref
	newProof.Scopes = append([]authority.ScopedEpoch(nil), f.base.proof.Scopes...)
	newProof.Scopes[2].Epoch = 6
	newProof.Consent.Body.ConsentID = "consent-c2-v2"
	alignAuthorityConsent(t, &newProof, f.base.ownerA, 2)
	newRaw, newID := authoritySignedProof(t, newProof, 11)
	if result := f.repo.RegisterProof(ctx, newRaw, newID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("new-version register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	newRequest := f.base.request
	newRequest.ProofID = newID
	newRequest.Scopes = append([]authority.ScopedEpoch(nil), newProof.Scopes...)
	if result := f.repo.VerifyProof(ctx, newRequest); result.Outcome != authority.OutcomeValid {
		t.Fatalf("new-version verify outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if _, result := f.repo.RotateCredential(ctx, authorityRotationCommand(t, f, "rotate-governance-aba", f.base.governance.Ref, replacement, 5, 1, "ABA replay")); result.Outcome != authority.OutcomeStale {
		t.Fatalf("ABA rotate outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	restarted := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: f.trust})
	if result := restarted.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeStale {
		t.Fatalf("restart old proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	epoch, result := restarted.ReadEpoch(ctx, "tenant-a", authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "credential-scope"})
	if result.Outcome != authority.OutcomeValid || epoch.Epoch != 6 || epoch.RowVersion != 2 {
		t.Fatalf("rotated epoch=%+v result=%+v", epoch, result)
	}
}

func TestAuthorityMySQL_ConcurrentRevocationAndVerification(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	const callers = 24
	start := make(chan struct{})
	outcomes := make(chan authority.Outcome, callers+1)
	var wg sync.WaitGroup
	wg.Add(callers + 1)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			outcomes <- f.repo.VerifyProof(ctx, f.base.request).Outcome
		}()
	}
	go func() {
		defer wg.Done()
		<-start
		_, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "revoke-concurrent",
			authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, 4, 1, "concurrent withdrawal"))
		outcomes <- result.Outcome
	}()
	close(start)
	wg.Wait()
	close(outcomes)
	for outcome := range outcomes {
		switch outcome {
		case authority.OutcomeValid, authority.OutcomeRevoked:
		default:
			t.Fatalf("unexpected concurrent outcome %s", outcome)
		}
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeRevoked {
		t.Fatalf("post-race outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	epoch, result := f.repo.ReadEpoch(ctx, "tenant-a", authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"})
	if result.Outcome != authority.OutcomeValid || epoch.Epoch != 5 || !epoch.Revoked {
		t.Fatalf("post-race epoch=%+v result=%+v", epoch, result)
	}
}

func alignAuthorityConsent(t *testing.T, proof *authority.Proof, owner authority.Credential, seed byte) {
	t.Helper()
	body := proof.Consent.Body
	body.DelegatedSubject = proof.Subject
	body.Resource = proof.Resource
	body.Action = proof.Action
	body.Tenant = proof.Tenant
	body.Operation = proof.Operation
	body.CaseID = proof.CaseID
	body.Round = proof.Round
	body.ExecutionGeneration = proof.ExecutionGeneration
	body.Scopes = append([]authority.ScopedEpoch(nil), proof.Scopes...)
	proof.Consent = authorityConsent(t, body, authorityApproval(t, body, owner, seed))
}

func assertAuthorityProofNotPersisted(t *testing.T, f *authoritySQLFixture, proofID string) {
	t.Helper()
	var count int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM magi_authority_proof WHERE trust_domain = ? AND proof_id = ?`,
		"tenant-a", proofID).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("proof %s persisted count=%d err=%v", proofID, count, err)
	}
}

func TestAuthorityMySQL_RejectsInvalidProofChainsAndBindings(t *testing.T) {
	cases := []struct {
		name     string
		outcome  authority.Outcome
		code     string
		mutate   func(t *testing.T, f *authoritySQLFixture) authority.Proof
		signSeed byte
	}{
		{
			name: "scope widening", outcome: authority.OutcomeDenied, code: "DELEGATION_SCOPE_WIDENED", signSeed: 3,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.Issuer = f.base.delegateA.Subject
				proof.Subject = f.base.delegateA.Subject
				proof.Signer = f.base.delegateA.Ref
				proof.Resource = "case/99"
				proof.Delegations = []authority.SignedDelegation{signAuthorityDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "widened-hop",
					TrustDomain: "tenant-a", RootIncarnation: "root-c2",
					Issuer: f.base.governance.Subject, IssuerCredential: f.base.governance.Ref,
					Delegate:  f.base.delegateA.Subject,
					Resources: []string{"case/99"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"}, Operations: []string{"PUBLISH_ARTIFACT"},
					NotBefore: authorityTime(f.base.now.Add(-5 * time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(time.Hour)), Delegable: false,
				}, 1)}
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
		{
			name: "delegation cycle", outcome: authority.OutcomeConflict, code: "DELEGATION_CYCLE", signSeed: 1,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				first := signAuthorityDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "cycle-1",
					TrustDomain: "tenant-a", RootIncarnation: "root-c2",
					Issuer: f.base.governance.Subject, IssuerCredential: f.base.governance.Ref,
					Delegate:  f.base.delegateA.Subject,
					Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"}, Operations: []string{"PUBLISH_ARTIFACT"},
					NotBefore: authorityTime(f.base.now.Add(-5 * time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(time.Hour)), Delegable: true,
				}, 1)
				second := signAuthorityDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "cycle-2",
					TrustDomain: "tenant-a", RootIncarnation: "root-c2",
					Issuer: f.base.delegateA.Subject, IssuerCredential: f.base.delegateA.Ref,
					Delegate:  f.base.governance.Subject,
					Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"}, Operations: []string{"PUBLISH_ARTIFACT"},
					NotBefore: authorityTime(f.base.now.Add(-4 * time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(50 * time.Minute)), Delegable: false,
				}, 3)
				proof.Delegations = []authority.SignedDelegation{first, second}
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
		{
			name: "bad owner consent", outcome: authority.OutcomeDenied, code: "OWNER_CONSENT_WRONG_OWNER", signSeed: 1,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.Consent = authorityConsent(t, proof.Consent.Body, authorityApproval(t, proof.Consent.Body, f.base.ownerB, 7))
				return proof
			},
		},
		{
			name: "wrong subject", outcome: authority.OutcomeDenied, code: "PROOF_ISSUER_MISMATCH", signSeed: 3,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.Issuer = f.base.delegateA.Subject
				proof.Subject = f.base.delegateA.Subject
				proof.Signer = f.base.delegateA.Ref
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
		{
			name: "expired proof", outcome: authority.OutcomeExpired, code: "PROOF_EXPIRED", signSeed: 1,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.IssuedAt = authorityTime(f.base.now.Add(-2 * time.Hour))
				proof.ExpiresAt = authorityTime(f.base.now.Add(-time.Hour))
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
		{
			name: "stale epoch", outcome: authority.OutcomeStale, code: "SCOPED_EPOCH_STALE", signSeed: 1,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.Scopes[0].Epoch = 2
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
		{
			name: "generation confused with epoch", outcome: authority.OutcomeStale, code: "SCOPED_EPOCH_STALE", signSeed: 1,
			mutate: func(t *testing.T, f *authoritySQLFixture) authority.Proof {
				proof := f.base.proof
				proof.Scopes[1].Epoch = proof.ExecutionGeneration
				alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
				return proof
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthorityMySQL(t)
			ctx := context.Background()
			if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
				t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
			}
			proof := tc.mutate(t, f)
			raw, id := authoritySignedProof(t, proof, tc.signSeed)
			result := f.repo.RegisterProof(ctx, raw, id)
			if result.Outcome != tc.outcome || result.Code != tc.code {
				t.Fatalf("outcome=%s code=%s detail=%s, want %s/%s", result.Outcome, result.Code, result.Detail, tc.outcome, tc.code)
			}
			assertAuthorityProofNotPersisted(t, f, id)
		})
	}
}

func signAuthorityDelegation(t *testing.T, body authority.DelegationBody, seed byte) authority.SignedDelegation {
	t.Helper()
	msg := append([]byte("magi:authority-delegation-signature:v1\n"), authorityCanonical(t, body)...)
	return authority.SignedDelegation{
		Body: body, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(authoritySeed(seed), msg)),
	}
}

func TestAuthorityMySQL_RoundGenerationAndEpochAreIndependent(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	cases := []struct {
		name   string
		mutate func(*authority.VerificationRequest)
	}{
		{"round", func(r *authority.VerificationRequest) { r.Round = r.ExecutionGeneration }},
		{"generation", func(r *authority.VerificationRequest) { r.ExecutionGeneration = r.Round }},
		{"authority epoch as context", func(r *authority.VerificationRequest) { r.Scopes[1].Epoch = r.Scopes[0].Epoch }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := f.base.request
			req.Scopes = append([]authority.ScopedEpoch(nil), f.base.request.Scopes...)
			tc.mutate(&req)
			result := f.repo.VerifyProof(ctx, req)
			if result.Outcome != authority.OutcomeStale {
				t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
			}
		})
	}
}

func TestAuthorityMySQL_ConcurrentKeyRotationAndVerification(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope",
		f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	const callers = 24
	start := make(chan struct{})
	outcomes := make(chan authority.Outcome, callers+1)
	var wg sync.WaitGroup
	wg.Add(callers + 1)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			outcomes <- f.repo.VerifyProof(ctx, f.base.request).Outcome
		}()
	}
	go func() {
		defer wg.Done()
		<-start
		_, result := f.repo.RotateCredential(ctx, authorityRotationCommand(t, f, "rotate-governance-concurrent",
			f.base.governance.Ref, replacement, 5, 1, "concurrent rotation"))
		outcomes <- result.Outcome
	}()
	close(start)
	wg.Wait()
	close(outcomes)
	for outcome := range outcomes {
		switch outcome {
		case authority.OutcomeValid, authority.OutcomeStale:
		default:
			t.Fatalf("unexpected concurrent rotation outcome %s", outcome)
		}
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeStale {
		t.Fatalf("post-rotation outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestAuthorityMySQL_CapabilityOffFailsClosed(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	disabled := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{})
	if _, _, result := disabled.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeDenied || result.Code != "AUTHORITY_CAPABILITY_DISABLED" {
		t.Fatalf("disabled bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := disabled.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeDenied || result.Code != "AUTHORITY_CAPABILITY_DISABLED" {
		t.Fatalf("disabled verify outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	var count int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM magi_authority_root`).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("disabled repository persisted roots=%d err=%v", count, err)
	}
}

func TestAuthorityMySQL_UntrustedBootstrapDoesNotInstallRoot(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	untrusted := f.trust
	untrusted.PublicKey = base64.StdEncoding.EncodeToString(authoritySeed(12).Public().(ed25519.PublicKey))
	repo := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: untrusted})
	if _, _, result := repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_SIGNATURE_INVALID" {
		t.Fatalf("untrusted bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	var roots int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM magi_authority_root`).Scan(&roots).Error; err != nil {
		t.Fatal(err)
	}
	if roots != 0 {
		t.Fatalf("untrusted bootstrap persisted roots=%d", roots)
	}
}

func TestAuthorityMySQL_StoredProofTamperFailsClosed(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	replacement := f.base.proof
	replacement.Action = "DELETE"
	alignAuthorityConsent(t, &replacement, f.base.ownerA, 2)
	replacementRaw, replacementID := authoritySignedProof(t, replacement, 1)
	if replacementID == f.base.proofID {
		t.Fatal("test replacement unexpectedly has the original proof id")
	}
	if err := f.db.Exec(`UPDATE magi_authority_proof SET raw_proof = ? WHERE trust_domain = ? AND proof_id = ?`,
		replacementRaw, "tenant-a", f.base.proofID).Error; err != nil {
		t.Fatal(err)
	}
	if _, result := f.repo.ReadProof(ctx, "tenant-a", f.base.proofID); result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_IDENTITY_MISMATCH" {
		t.Fatalf("readback tamper outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := f.repo.VerifyProof(ctx, f.base.request); result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_IDENTITY_MISMATCH" {
		t.Fatalf("verify tamper outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
}

func TestAuthorityMySQL_EvidenceTamperFailsClosed(t *testing.T) {
	t.Run("proof scope evidence", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		if err := f.db.Exec(`UPDATE magi_authority_proof_scope SET epoch = epoch + 1
			WHERE trust_domain = ? AND proof_id = ? AND scope_kind = 'CONTEXT'`, "tenant-a", f.base.proofID).Error; err != nil {
			t.Fatal(err)
		}
		result := f.repo.VerifyProof(ctx, f.base.request)
		if result.Outcome != authority.OutcomeConflict || result.Code != "PROOF_SCOPE_EVIDENCE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("consent evidence", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		if err := f.db.Exec(`UPDATE magi_authority_proof_consent SET raw_consent = ?
			WHERE trust_domain = ? AND proof_id = ?`, []byte("{}"), "tenant-a", f.base.proofID).Error; err != nil {
			t.Fatal(err)
		}
		result := f.repo.VerifyProof(ctx, f.base.request)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_EVIDENCE_CORRUPT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("delegation evidence", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		proof := f.base.proof
		proof.Issuer = f.base.delegateA.Subject
		proof.Subject = f.base.delegateA.Subject
		proof.Signer = f.base.delegateA.Ref
		proof.Delegations = []authority.SignedDelegation{signAuthorityDelegation(t, authority.DelegationBody{
			Version: authority.DelegationVersion, DelegationID: "evidence-hop",
			TrustDomain: "tenant-a", RootIncarnation: "root-c2",
			Issuer: f.base.governance.Subject, IssuerCredential: f.base.governance.Ref,
			Delegate:  f.base.delegateA.Subject,
			Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"}, Operations: []string{"PUBLISH_ARTIFACT"},
			NotBefore: authorityTime(f.base.now.Add(-5 * time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(time.Hour)), Delegable: false,
		}, 1)}
		alignAuthorityConsent(t, &proof, f.base.ownerA, 2)
		raw, id := authoritySignedProof(t, proof, 3)
		if result := f.repo.RegisterProof(ctx, raw, id); result.Outcome != authority.OutcomeValid {
			t.Fatalf("register delegated proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		if err := f.db.Exec(`UPDATE magi_authority_proof_delegation SET raw_delegation = ?
			WHERE trust_domain = ? AND proof_id = ?`, []byte("{}"), "tenant-a", id).Error; err != nil {
			t.Fatal(err)
		}
		req := f.base.request
		req.ProofID = id
		req.Subject = proof.Subject
		result := f.repo.VerifyProof(ctx, req)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_EVIDENCE_CORRUPT" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func authorityRevocationCommand(t *testing.T, f *authoritySQLFixture, commandID string, scope authority.ScopeKey, expectedEpoch, expectedRowVersion uint64, reason string) authority.SignedRevocationCommand {
	t.Helper()
	body := authority.RevocationCommandBody{
		Version: authority.RevocationCommandVersion, CommandID: commandID,
		TrustDomain: "tenant-a", RootIncarnation: "root-c2", Scope: scope,
		ExpectedEpoch: expectedEpoch, ExpectedRowVersion: expectedRowVersion, Reason: reason,
		IssuedAt: authorityTime(f.base.now.Add(-time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(time.Minute)),
	}
	msg := append([]byte("magi:authority-revocation-command-signature:v1\n"), authorityCanonical(t, body)...)
	return authority.SignedRevocationCommand{
		Body: body, Signer: f.base.governance.Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(authoritySeed(1), msg)),
	}
}

func authorityRotationCommand(t *testing.T, f *authoritySQLFixture, commandID string, old authority.CredentialRef, replacement authority.Credential, expectedEpoch, expectedRowVersion uint64, reason string) authority.SignedCredentialRotationCommand {
	t.Helper()
	body := authority.CredentialRotationCommandBody{
		Version: authority.CredentialRotationCommandVersion, CommandID: commandID,
		TrustDomain: "tenant-a", RootIncarnation: "root-c2",
		OldCredential: old, NewCredential: replacement,
		ExpectedEpoch: expectedEpoch, ExpectedRowVersion: expectedRowVersion, Reason: reason,
		IssuedAt: authorityTime(f.base.now.Add(-time.Minute)), ExpiresAt: authorityTime(f.base.now.Add(time.Minute)),
	}
	msg := append([]byte("magi:authority-credential-rotation-command-signature:v1\n"), authorityCanonical(t, body)...)
	return authority.SignedCredentialRotationCommand{
		Body: body, Signer: f.base.governance.Ref,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(authoritySeed(1), msg)),
	}
}

func TestAuthorityMySQL_UnauthenticatedMutationFailsClosed(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	revocation := authorityRevocationCommand(t, f, "revoke-forged",
		authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, 4, 1, "forged")
	revocation.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if _, result := f.repo.RevokeScope(ctx, revocation); result.Outcome != authority.OutcomeDenied || result.Code != "REVOCATION_SIGNATURE_INVALID" {
		t.Fatalf("forged revoke outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	epoch, result := f.repo.ReadEpoch(ctx, "tenant-a", authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"})
	if result.Outcome != authority.OutcomeValid || epoch.Epoch != 4 || epoch.Revoked || epoch.RowVersion != 1 {
		t.Fatalf("forged revocation changed epoch=%+v result=%+v", epoch, result)
	}
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope",
		f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	rotation := authorityRotationCommand(t, f, "rotate-forged", f.base.governance.Ref, replacement, 5, 1, "forged")
	rotation.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if _, result := f.repo.RotateCredential(ctx, rotation); result.Outcome != authority.OutcomeDenied || result.Code != "CREDENTIAL_ROTATION_SIGNATURE_INVALID" {
		t.Fatalf("forged rotate outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	var activeOld int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM magi_authority_credential
		WHERE trust_domain = ? AND credential_id = ? AND credential_version = 1 AND status = 'ACTIVE'`,
		"tenant-a", "cred-governance").Scan(&activeOld).Error; err != nil || activeOld != 1 {
		t.Fatalf("forged rotation changed credential count=%d err=%v", activeOld, err)
	}
	proof, parseResult := authority.ParseSignedProof(f.base.proofRaw, f.base.proofID)
	if parseResult.Outcome != authority.OutcomeValid {
		t.Fatal(parseResult)
	}
	forgedProofRaw, forgedProofID := authoritySignedProof(t, proof.Proof, 1)
	var envelope authority.SignedProof
	if err := json.Unmarshal(forgedProofRaw, &envelope); err != nil {
		t.Fatal(err)
	}
	forgedSignature, _ := base64.StdEncoding.DecodeString(envelope.Signature)
	forgedSignature[0] ^= 1
	envelope.Signature = base64.StdEncoding.EncodeToString(forgedSignature)
	canonicalEnvelope, err := canonical.CanonicalizeStruct(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result := f.repo.RegisterProof(ctx, canonicalEnvelope.Bytes(), forgedProofID); result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_SIGNATURE_INVALID" {
		t.Fatalf("forged proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	assertAuthorityProofNotPersisted(t, f, forgedProofID)
}

func TestAuthorityMySQL_ReusedConsentEvidenceIsConflict(t *testing.T) {
	f := newAuthorityMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	if result := f.repo.RegisterProof(ctx, f.base.proofRaw, f.base.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	second := f.base.proof
	second.ExecutionGeneration = 8
	alignAuthorityConsent(t, &second, f.base.ownerA, 2)
	secondRaw, secondID := authoritySignedProof(t, second, 1)
	result := f.repo.RegisterProof(ctx, secondRaw, secondID)
	if result.Outcome != authority.OutcomeConflict || result.Code != "CONSENT_EVIDENCE_CONFLICT" {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	assertAuthorityProofNotPersisted(t, f, secondID)
}

func TestAuthorityMySQL_MigrationIsAdditiveAndIdempotent(t *testing.T) {
	f := newAuthorityMySQL(t)
	applyAuthorityMigrationFile(t, f.db)
	if f.db.Migrator().HasTable("magi_authority_sequence") {
		t.Fatal("S32 must not create a second authority sequence; #47 owns common C2/C3 order")
	}
	for _, table := range []string{
		"magi_authority_root", "magi_authority_credential",
		"magi_authority_owner_policy", "magi_authority_epoch", "magi_authority_proof",
		"magi_authority_proof_scope", "magi_authority_proof_delegation", "magi_authority_proof_consent",
		"magi_authority_command",
	} {
		if !f.db.Migrator().HasTable(table) {
			t.Fatalf("migration did not create %s", table)
		}
	}
	for _, table := range []string{
		"magi_authority_root", "magi_authority_credential",
		"magi_authority_owner_policy", "magi_authority_epoch", "magi_authority_proof",
		"magi_authority_command",
	} {
		var count int64
		if err := f.db.Raw("SELECT COUNT(*) FROM " + table).Scan(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s seeded rows count=%d err=%v", table, count, err)
		}
	}
}
