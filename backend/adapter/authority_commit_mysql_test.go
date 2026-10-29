package magi_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gosqlmysql "github.com/go-sql-driver/mysql"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
	"github.com/jamespud/magi/backend/domain/entity"
)

// Keep newAuthorityMySQL S32-only: only consumers of the common order opt in.
func newAuthorityCommitMySQL(t *testing.T) *authoritySQLFixture {
	t.Helper()
	f := newAuthorityMySQL(t)
	applyAuthorityCommitMigration(t, f)
	return f
}

func applyAuthorityCommitMigration(t *testing.T, f *authoritySQLFixture) {
	t.Helper()
	applyAuthorityMigration(t, f, "../../docker/atlas/migrations/magi_s33_authority_commit.sql")
	applyAuthorityMigration(t, f, "../../docker/atlas/migrations/magi_s34_authority_exposure_gate.sql")
}

func TestAuthorityCommitMySQL_SchemaHasCommonOrderAndNoSettlement(t *testing.T) {
	f := newAuthorityMySQL(t)
	before, err := f.db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	applyAuthorityCommitMigration(t, f)
	applyAuthorityCommitMigration(t, f) // additive and repeatable
	after, err := f.db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	prior := make(map[string]bool)
	for _, name := range before {
		prior[name] = true
	}
	var added []string
	for _, name := range after {
		if !prior[name] {
			added = append(added, name)
		}
	}
	want := []string{
		"magi_authority_head", "magi_authority_journal_v2", "magi_authority_journal_event_v2",
		"magi_authority_admission_v2", "magi_authority_dependency_v2",
		"magi_authority_provider_reservation_v2", "magi_authority_exposure_fence_v2",
		"magi_authority_pending_transition_v2", "magi_authority_unique_binding_v2",
		"magi_authority_token_consumed_v2", "magi_authority_lineage_v2", "magi_authority_outbox_v2",
		"magi_authority_exposure_gate_attempt_v2", "magi_authority_exposure_gate_commit_v2",
		"magi_authority_exposure_gate_lineage_v2", "magi_authority_exposure_gate_token_v2",
		"magi_authority_exposure_gate_attestation_v2",
	}
	sort.Strings(added)
	sort.Strings(want)
	if strings.Join(added, ",") != strings.Join(want, ",") {
		t.Fatalf("added tables=%v want=%v", added, want)
	}
	if f.db.Migrator().HasTable("magi_provider_settlement_v2") {
		t.Fatal("settlement must remain absent")
	}
	for _, table := range want {
		var count int64
		if err := f.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s seeded rows=%d err=%v", table, count, err)
		}
		var engine string
		if err := f.db.Raw(`SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, table).Scan(&engine).Error; err != nil || engine != "InnoDB" {
			t.Fatalf("%s engine=%s err=%v", table, engine, err)
		}
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain) VALUES ('tenant-a')`).Error; err != nil {
		t.Fatal(err)
	}
	assertAuthorityHead(t, f, 0, 0, 0, 0)
	for _, table := range []string{"magi_authority_admission_v2", "magi_authority_unique_binding_v2"} {
		for _, column := range []string{"request_digest", "exact_target_id", "commit_token", "commit_id", "authority_sequence", "journal_high_watermark"} {
			if !f.db.Migrator().HasColumn(table, column) {
				t.Errorf("%s missing %s", table, column)
			}
		}
	}
	// Valid declaration modes reach the admission FK; unsupported modes fail
	// the CHECK first. Even a valid declaration cannot exist without admission.
	for _, mode := range []string{"PROVIDER_ENFORCED_LEASE", "AUTHORITY_ORDERED_GATE", "QUERY_ONLY"} {
		err := f.db.Exec(`INSERT INTO magi_authority_exposure_fence_v2
            (trust_domain, commit_id, fence_id, mode, lease_id, lease_until,
             authority_watermark, execution_generation, scoped_epochs, provider_capability_id, provider_capability_digest, provider_capability_ref)
            VALUES ('tenant-a', 'missing', 'fence', ?, 'lease', CURRENT_TIMESTAMP(6), 0, 7, '[]', 'cap', REPEAT('a',64), 'object/cap')`, mode).Error
		wantError := uint16(1452)
		if mode == "QUERY_ONLY" {
			wantError = 3819
		}
		if !authorityCommitMySQLError(err, wantError) {
			t.Fatalf("mode %s: want MySQL %d got %v", mode, wantError, err)
		}
	}
	for _, blocked := range []struct {
		state     string
		permitted int
	}{
		{"CONSUMED", 0}, {"DECLARED_BLOCKED", 1},
	} {
		err := f.db.Exec(`INSERT INTO magi_authority_exposure_fence_v2
            (trust_domain, commit_id, fence_id, mode, state, exposure_permitted, lease_id, lease_until,
             authority_watermark, execution_generation, scoped_epochs, provider_capability_id, provider_capability_digest, provider_capability_ref)
            VALUES ('tenant-a', 'missing', 'fence', 'PROVIDER_ENFORCED_LEASE', ?, ?, 'lease', CURRENT_TIMESTAMP(6), 0, 7, '[]', 'cap', REPEAT('a',64), 'object/cap')`, blocked.state, blocked.permitted).Error
		if !authorityCommitMySQLError(err, 3819) {
			t.Fatalf("fence %+v: want CHECK rejection got %v", blocked, err)
		}
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_provider_reservation_v2
        (trust_domain, commit_id, provider_request_id, session_scope_id, state)
        VALUES ('tenant-a', 'missing', 'req', 'session', 'APPLIED_VERIFIED')`).Error; !authorityCommitMySQLError(err, 3819) {
		t.Fatalf("verified reservation must violate CHECK, got %v", err)
	}
	testAuthorityCommitSchemaBindings(t, f)
}

func assertAuthorityHead(t *testing.T, f *authoritySQLFixture, sequence, admission, revocation, gate uint64) {
	t.Helper()
	var h struct{ LastSequence, LastAdmissionSequence, LastRevocationSequence, LastGateSequence, LastSettlementSequence uint64 }
	result := f.db.Raw(`SELECT last_sequence, last_admission_sequence, last_revocation_sequence, last_gate_sequence, last_settlement_sequence FROM magi_authority_head WHERE trust_domain='tenant-a'`).Scan(&h)
	if result.Error != nil || result.RowsAffected != 1 || h.LastSequence != sequence || h.LastAdmissionSequence != admission || h.LastRevocationSequence != revocation || h.LastGateSequence != gate || h.LastSettlementSequence != 0 {
		t.Fatalf("head=%+v rows=%d err=%v want sequence=%d admission=%d revocation=%d gate=%d", h, result.RowsAffected, result.Error, sequence, admission, revocation, gate)
	}
}

func assertAuthorityHeadZeroOrAbsent(t *testing.T, f *authoritySQLFixture) {
	t.Helper()
	var count int64
	if err := f.db.Table("magi_authority_head").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		return
	}
	assertAuthorityHead(t, f, 0, 0, 0, 0)
}

func TestAuthorityCommitMySQL_RevocationAndRotationAdvanceCommonSequence(t *testing.T) {
	t.Run("missing S33 fails closed", func(t *testing.T) {
		f := newAuthorityMySQL(t)
		ctx := context.Background()
		if f.db.Migrator().HasTable("magi_authority_head") {
			t.Fatal("S32 fixture unexpectedly has common order")
		}
		if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
		_, result := f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "no-s33", scope, 4, 1, "withdraw"))
		if result.Outcome != authority.OutcomeUnknown || result.Code != "AUTHORITY_STORE_ERROR" {
			t.Fatal(result)
		}
		epoch, read := f.repo.ReadEpoch(ctx, "tenant-a", scope)
		if read.Outcome != authority.OutcomeValid || epoch.Epoch != 4 || epoch.RowVersion != 1 || epoch.Revoked {
			t.Fatalf("missing-order mutation: %+v result=%+v", epoch, read)
		}
		replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope", f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
		_, result = f.repo.RotateCredential(ctx, authorityRotationCommand(t, f, "no-s33-rotate", f.base.governance.Ref, replacement, 5, 1, "rotate"))
		if result.Outcome != authority.OutcomeUnknown || result.Code != "AUTHORITY_STORE_ERROR" {
			t.Fatal(result)
		}
		var commands int64
		if err := f.db.Table("magi_authority_command").Count(&commands).Error; err != nil || commands != 0 {
			t.Fatalf("missing-order commands=%d err=%v", commands, err)
		}
	})
	t.Run("revocation waits before root", func(t *testing.T) { testAuthorityCommitOrderedWriter(t, false) })
	t.Run("rotation waits before root", func(t *testing.T) { testAuthorityCommitOrderedWriter(t, true) })
	t.Run("revocation sequence failure rolls back", func(t *testing.T) { testAuthorityCommitSequenceFailureRollsBack(t, false) })
	t.Run("rotation sequence failure rolls back", func(t *testing.T) { testAuthorityCommitSequenceFailureRollsBack(t, true) })
	f := newAuthorityCommitMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	revocation := authorityRevocationCommand(t, f, "ordered-revoke", scope, 4, 1, "withdraw")
	first, result := f.repo.RevokeScope(ctx, revocation)
	if result.Outcome != authority.OutcomeValid || first.Epoch != 5 || !first.Revoked {
		t.Fatalf("revoke=%+v result=%+v", first, result)
	}
	assertAuthorityHead(t, f, 1, 0, 1, 0)
	replay, result := f.repo.RevokeScope(ctx, revocation)
	if result.Outcome != authority.OutcomeValid || replay != first {
		t.Fatalf("revoke replay=%+v result=%+v", replay, result)
	}
	assertAuthorityHead(t, f, 1, 0, 1, 0)
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope", f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	rotation := authorityRotationCommand(t, f, "ordered-rotate", f.base.governance.Ref, replacement, 5, 1, "rotate")
	rotated, result := f.repo.RotateCredential(ctx, rotation)
	if result.Outcome != authority.OutcomeValid || rotated.Ref != replacement.Ref {
		t.Fatalf("rotate=%+v result=%+v", rotated, result)
	}
	assertAuthorityHead(t, f, 2, 0, 2, 0)
	// A fresh repository must recover exact command results after key retirement.
	restarted := magi.NewAuthorityRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: f.trust})
	rotated, result = restarted.RotateCredential(ctx, rotation)
	if result.Outcome != authority.OutcomeValid || rotated.Ref != replacement.Ref {
		t.Fatalf("rotation replay=%+v result=%+v", rotated, result)
	}
	replay, result = restarted.RevokeScope(ctx, revocation)
	if result.Outcome != authority.OutcomeValid || replay != first {
		t.Fatalf("revoke after rotation=%+v result=%+v", replay, result)
	}
	assertAuthorityHead(t, f, 2, 0, 2, 0)
	changed := authorityRotationCommand(t, f, "ordered-rotate", f.base.governance.Ref, replacement, 5, 1, "changed")
	if _, result := restarted.RotateCredential(ctx, changed); result.Outcome != authority.OutcomeConflict {
		t.Fatal(result)
	}
	assertAuthorityHead(t, f, 2, 0, 2, 0)
	var commands int64
	if err := f.db.Table("magi_authority_command").Count(&commands).Error; err != nil || commands != 2 {
		t.Fatalf("commands=%d err=%v", commands, err)
	}
}

func authorityCommitMySQLError(err error, number uint16) bool {
	var mysqlErr *gosqlmysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == number
}

func testAuthorityCommitOrderedWriter(t *testing.T, rotation bool) {
	t.Helper()
	f := newAuthorityCommitMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain) VALUES ('tenant-a')`).Error; err != nil {
		t.Fatal(err)
	}
	blocker := f.db.WithContext(ctx).Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var sequence uint64
	if err := blocker.Raw(`SELECT last_sequence FROM magi_authority_head WHERE trust_domain='tenant-a' FOR UPDATE`).Row().Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	revocation := authorityRevocationCommand(t, f, "wait-revoke", scope, 4, 1, "withdraw")
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope", f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	command := authorityRotationCommand(t, f, "wait-rotate", f.base.governance.Ref, replacement, 5, 1, "rotate")
	done := make(chan authority.Result, 1)
	go func() {
		if rotation {
			_, result := f.repo.RotateCredential(ctx, command)
			done <- result
		} else {
			_, result := f.repo.RevokeScope(ctx, revocation)
			done <- result
		}
	}()
	// Release all gates before bounded cleanup even if an assertion fails.
	defer func() {
		_ = blocker.Rollback().Error
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("ordered writer did not exit")
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int64
		err := f.db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w
            JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE
            WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='magi_authority_head'`).Scan(&waiting).Error
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not observably wait on common head")
		}
		time.Sleep(10 * time.Millisecond)
	}
	rootProbe := f.db.WithContext(ctx).Begin()
	if rootProbe.Error != nil {
		t.Fatal(rootProbe.Error)
	}
	var root string
	err := rootProbe.Raw(`SELECT root_incarnation FROM magi_authority_root WHERE trust_domain='tenant-a' AND active_slot=1 FOR UPDATE NOWAIT`).Row().Scan(&root)
	_ = rootProbe.Rollback().Error
	if err != nil || root != "root-c2" {
		t.Fatalf("waiting writer locked root before head: root=%s err=%v", root, err)
	}
	assertAuthorityHead(t, f, 0, 0, 0, 0)
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		// Keep the deferred drain valid after observing the result here.
		done <- result
		if result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertAuthorityHead(t, f, 1, 0, 1, 0)
}

func testAuthorityCommitSequenceFailureRollsBack(t *testing.T, rotation bool) {
	t.Helper()
	f := newAuthorityCommitMySQL(t)
	ctx := context.Background()
	if _, _, result := f.repo.BootstrapRoot(ctx, f.base.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	// Exhaustion is detected only after epoch/credential and command writes.
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain, last_sequence) VALUES ('tenant-a', 18446744073709551615)`).Error; err != nil {
		t.Fatal(err)
	}
	var result authority.Result
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	expectedEpoch := uint64(4)
	if rotation {
		scope = authority.ScopeKey{Kind: authority.ScopeCredential, ScopeID: "credential-scope"}
		expectedEpoch = 5
		replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope", f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
		_, result = f.repo.RotateCredential(ctx, authorityRotationCommand(t, f, "exhausted-rotate", f.base.governance.Ref, replacement, 5, 1, "rotate"))
		var active int64
		if err := f.db.Table("magi_authority_credential").Where("credential_id=? AND credential_version=1 AND status='ACTIVE'", "cred-governance").Count(&active).Error; err != nil || active != 1 {
			t.Fatalf("old credential active=%d err=%v", active, err)
		}
		var replacements int64
		if err := f.db.Table("magi_authority_credential").Where("credential_id=? AND credential_version=2", "cred-governance").Count(&replacements).Error; err != nil || replacements != 0 {
			t.Fatalf("new credential rows=%d err=%v", replacements, err)
		}
	} else {
		_, result = f.repo.RevokeScope(ctx, authorityRevocationCommand(t, f, "exhausted-revoke", scope, 4, 1, "withdraw"))
	}
	if result.Outcome != authority.OutcomeConflict || result.Code != "AUTHORITY_SEQUENCE_EXHAUSTED" {
		t.Fatal(result)
	}
	epoch, read := f.repo.ReadEpoch(ctx, "tenant-a", scope)
	if read.Outcome != authority.OutcomeValid || epoch.Epoch != expectedEpoch || epoch.RowVersion != 1 || epoch.Revoked {
		t.Fatalf("partial epoch=%+v result=%+v", epoch, read)
	}
	var commands int64
	if err := f.db.Table("magi_authority_command").Count(&commands).Error; err != nil || commands != 0 {
		t.Fatalf("partial command rows=%d err=%v", commands, err)
	}
	assertAuthorityHead(t, f, ^uint64(0), 0, 0, 0)
}

// These are direct DDL probes, not an implementation of the admission consumer.
func testAuthorityCommitSchemaBindings(t *testing.T, f *authoritySQLFixture) {
	t.Helper()
	journal := map[string]any{
		"trust_domain": "tenant-a", "journal_id": "journal-1", "case_id": "case-42", "case_round": 2,
		"context_manifest_id": "context-1", "persona_id": "melchior", "execution_generation": 7,
	}
	if err := f.db.Table("magi_authority_journal_v2").Create(journal).Error; err != nil {
		t.Fatal(err)
	}
	admission := map[string]any{
		"trust_domain": "tenant-a", "commit_id": "commit-1", "unique_key": strings.Repeat("b", 64),
		"case_id": "case-42", "case_round": 2, "context_manifest_id": "context-1", "persona_id": "melchior",
		"delivery_id": "delivery-1", "provider_request_id": "request-1", "journal_id": "journal-1",
		"request_digest": strings.Repeat("a", 64), "exact_target_id": "admission-1", "commit_token": "token-1",
		"authority_sequence": 7, "journal_high_watermark": 1, "execution_generation": 7,
		"proof_id": "proof-1", "admission_digest": strings.Repeat("c", 64), "admission_object_ref": "object/admission",
		"raw_request": []byte("{}"), "owner_job_id": "job-1", "owner_worker_id": "worker-1", "owner_lease_until": time.Now().UTC().Add(time.Hour),
	}
	if err := f.db.Table("magi_authority_admission_v2").Create(admission).Error; err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		AuthoritySequence, JournalHighWatermark uint64
		ExposurePermitted                       bool
	}
	if err := f.db.Table("magi_authority_admission_v2").Where("trust_domain=? AND commit_id=?", "tenant-a", "commit-1").Take(&recorded).Error; err != nil || recorded.AuthoritySequence != 7 || recorded.JournalHighWatermark != 1 || recorded.ExposurePermitted {
		t.Fatalf("separate counters / default exposure: %+v err=%v", recorded, err)
	}
	// Provider request, token, digest and Generation cannot reset the six-field
	// identity, even if a consumer supplies a different projected unique key.
	retry := make(map[string]any, len(admission))
	for k, v := range admission {
		retry[k] = v
	}
	retry["commit_id"], retry["unique_key"] = "commit-2", strings.Repeat("d", 64)
	retry["provider_request_id"], retry["commit_token"] = "request-2", "token-2"
	retry["request_digest"], retry["execution_generation"] = strings.Repeat("e", 64), 8
	retry["authority_sequence"], retry["journal_high_watermark"] = 8, 2
	if err := f.db.Table("magi_authority_admission_v2").Create(retry).Error; !authorityCommitMySQLError(err, 1062) || !strings.Contains(err.Error(), "uk_ac_admission_identity") {
		t.Fatalf("six-field identity not enforced: %v", err)
	}
	// The same natural identity in a different trust domain is independent.
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain) VALUES ('tenant-b')`).Error; err != nil {
		t.Fatal(err)
	}
	journal["trust_domain"], admission["trust_domain"] = "tenant-b", "tenant-b"
	if err := f.db.Table("magi_authority_journal_v2").Create(journal).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Table("magi_authority_admission_v2").Create(admission).Error; err != nil {
		t.Fatalf("trust domain isolation: %v", err)
	}
}

type journalAdmitMySQLFixture struct {
	*authoritySQLFixture
	commitRepo   *magi.AuthorityCommitRepository
	owner        *entity.ExecutionContext
	request      authoritycommit.AdmissionRequest
	c2Request    authority.VerificationRequest
	auditRequest authoritycommit.ReplayRequest
	bootstrapRaw []byte
	proofRaw     []byte
	proofID      string
	proofExpires time.Time
}

func newJournalAdmitMySQL(t *testing.T) *journalAdmitMySQLFixture {
	return newJournalAdmitMySQLWithProofTTL(t, 30*time.Minute)
}

func newJournalAdmitMySQLWithProofTTL(t *testing.T, proofTTL time.Duration) *journalAdmitMySQLFixture {
	t.Helper()
	base := newAuthorityCommitMySQL(t)
	const (
		exactTarget    = "admission-r0"
		operation      = authoritycommit.JournalAdmitV2
		auditOperation = authoritycommit.JournalAuditRead
	)
	signed := newC2SignedFixture(t)
	auditScopes := []authority.ScopedEpoch{
		{Kind: authority.ScopeAuthority, ScopeID: "audit-authority-scope", Epoch: 1},
		{Kind: authority.ScopeContext, ScopeID: "audit-context-scope", Epoch: 2},
		{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
	}
	signed.policy.Resources = []string{exactTarget, "gate-r0"}
	signed.policy.Operations = []string{operation, auditOperation, authoritycommit.JournalExposureGateV2}
	proofExpires := time.Now().UTC().Add(proofTTL).Truncate(time.Millisecond)
	signed.proof.Resource = exactTarget
	signed.proof.Operation = operation
	signed.proof.ExpiresAt = authorityTime(proofExpires)
	signed.proof.Consent.Body.Resource = exactTarget
	signed.proof.Consent.Body.Operation = operation
	signed.proof.Consent.Body.ExpiresAt = authorityTime(proofExpires)
	alignAuthorityConsent(t, &signed.proof, signed.ownerA, 2)
	signed.proofRaw, signed.proofID = authoritySignedProof(t, signed.proof, 1)
	gateProof := signed.proof
	gateProof.Resource = "gate-r0"
	gateProof.Operation = authoritycommit.JournalExposureGateV2
	gateProof.Consent.Body.ConsentID = "consent-c2-gate"
	gateProof.Consent.Body.Resource = "gate-r0"
	gateProof.Consent.Body.Operation = authoritycommit.JournalExposureGateV2
	alignAuthorityConsent(t, &gateProof, signed.ownerA, 2)
	gateProofRaw, gateProofID := authoritySignedProof(t, gateProof, 1)
	auditProof := signed.proof
	auditProof.Operation = auditOperation
	auditProof.Scopes = append([]authority.ScopedEpoch(nil), auditScopes...)
	auditProof.Consent.Body.ConsentID = "consent-c2-audit"
	auditProof.Consent.Body.Operation = auditOperation
	auditProof.Consent.Body.Scopes = append([]authority.ScopedEpoch(nil), auditScopes...)
	alignAuthorityConsent(t, &auditProof, signed.ownerA, 2)
	auditProofRaw, auditProofID := authoritySignedProof(t, auditProof, 1)
	signed.bootstrap.RootGrant.Resources = []string{exactTarget, "gate-r0"}
	signed.bootstrap.RootGrant.Operations = []string{operation, auditOperation, authoritycommit.JournalExposureGateV2}
	signed.bootstrap.OwnerPolicies[0] = signed.policy
	signed.bootstrap.InitialScopes = append(append([]authority.ScopedEpoch(nil), signed.scopes...), auditScopes[0], auditScopes[1])
	sort.Slice(signed.bootstrap.InitialScopes, func(i, j int) bool {
		return authority.ScopeKeyCompare(
			authority.ScopeKey{Kind: signed.bootstrap.InitialScopes[i].Kind, ScopeID: signed.bootstrap.InitialScopes[i].ScopeID},
			authority.ScopeKey{Kind: signed.bootstrap.InitialScopes[j].Kind, ScopeID: signed.bootstrap.InitialScopes[j].ScopeID},
		) < 0
	})
	signed.bootstrapRaw, signed.trust = authoritySignedBootstrap(t, signed.bootstrap, 9)
	signed.request = authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: signed.proofID, Subject: signed.proof.Subject,
		Resource: exactTarget, Action: signed.proof.Action, Tenant: signed.proof.Tenant, Operation: operation,
		CaseID: signed.proof.CaseID, Round: signed.proof.Round, ExecutionGeneration: signed.proof.ExecutionGeneration,
		Scopes: append([]authority.ScopedEpoch(nil), signed.proof.Scopes...),
	}
	gateVerification := authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: gateProofID, Subject: gateProof.Subject,
		Resource: "gate-r0", Action: gateProof.Action, Tenant: gateProof.Tenant,
		Operation: authoritycommit.JournalExposureGateV2, CaseID: gateProof.CaseID,
		Round: gateProof.Round, ExecutionGeneration: gateProof.ExecutionGeneration,
		Scopes: append([]authority.ScopedEpoch(nil), gateProof.Scopes...),
	}
	auditVerification := authority.VerificationRequest{
		TrustDomain: "tenant-a", ProofID: auditProofID, Subject: auditProof.Subject,
		Resource: exactTarget, Action: auditProof.Action, Tenant: auditProof.Tenant, Operation: auditOperation,
		CaseID: auditProof.CaseID, Round: auditProof.Round, ExecutionGeneration: auditProof.ExecutionGeneration,
		Scopes: append([]authority.ScopedEpoch(nil), auditScopes...),
	}

	ctx := context.Background()
	if _, _, result := base.repo.BootstrapRoot(ctx, signed.bootstrapRaw); result.Outcome != authority.OutcomeValid {
		t.Fatalf("bootstrap outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := base.repo.RegisterProof(ctx, signed.proofRaw, signed.proofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := base.repo.RegisterProof(ctx, gateProofRaw, gateProofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register gate proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if result := base.repo.RegisterProof(ctx, auditProofRaw, auditProofID); result.Outcome != authority.OutcomeValid {
		t.Fatalf("register audit proof outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if err := base.db.AutoMigrate(&magi.CaseModel{}, &magi.DecisionJobModel{}); err != nil {
		t.Fatalf("migrate runtime owner tables: %v", err)
	}
	leaseUntil := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	owner := &entity.ExecutionContext{
		CaseID: "case-42", JobID: "job-42", WorkerID: "worker-a", ExecutionGeneration: 7,
	}
	if err := base.db.Create(&magi.CaseModel{
		ID: "case-42", Status: "INVESTIGATING", ExecutionGeneration: 7,
	}).Error; err != nil {
		t.Fatalf("create case owner: %v", err)
	}
	if err := base.db.Create(&magi.DecisionJobModel{
		ID: "job-42", CaseID: "case-42", Status: string(entity.DecisionJobRunning),
		WorkerID: "worker-a", LeaseUntil: &leaseUntil, ExecutionGeneration: 7,
		AvailableAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create job owner: %v", err)
	}

	dependencies := make([]authoritycommit.PreparedObject, 6)
	for i := range dependencies {
		dependencies[i] = authoritycommit.PreparedObject{
			ID:        "dependency-" + string(rune('a'+i)),
			Digest:    strings.Repeat(string(rune('1'+i)), 64),
			ObjectRef: "object:dependency-" + string(rune('a'+i)),
		}
	}
	requester := authoritycommit.AdmissionRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalAdmitV2,
		ExactTargetID:   exactTarget,
		CommitToken:     "commit-token-r0",
		Verification:    signed.request,
		Admission: authoritycommit.AdmissionPlan{
			Key: authoritycommit.AdmissionKey{
				CaseID: "case-42", Round: 2, ContextManifestID: "manifest-r0",
				PersonaID: "melchior", DeliveryID: "delivery-r0", ProviderRequestID: "request-r0",
			},
			Principal:           signed.proof.Subject,
			ExecutionGeneration: 7,
			ScopedEpochs:        append([]authority.ScopedEpoch(nil), signed.proof.Scopes...),
			MutationSet: []string{
				"admission", "dependency_edges", "exposure_fence", "journal_event",
				"journal_high_watermark", "lineage", "outbox", "pending_transition",
				"provider_reservation", "token_consumed", "unique_binding",
			},
			AdmissionInput: authoritycommit.PreparedObject{
				ID: exactTarget, Digest: strings.Repeat("a", 64), ObjectRef: "object:admission-r0",
			},
			JournalEventInput: authoritycommit.PreparedObject{
				ID: "event-r0", Digest: strings.Repeat("b", 64), ObjectRef: "object:event-r0",
			},
			DependencyEdgeInputs: dependencies,
			HighWatermark: authoritycommit.HighWatermarkPlan{
				JournalID: "journal-r0", Expected: 0, Next: 1,
			},
			ProviderReservation: authoritycommit.ProviderReservationPlan{
				RequestID: "request-r0", SessionScopeID: "session-r0",
				ExpectedStatus: "ABSENT", NextStatus: "RESERVED",
			},
			PendingTransition: authoritycommit.PendingTransitionPlan{
				JournalID: "journal-r0", Expected: 0, Next: 1,
			},
			Fence: authoritycommit.ExposureFencePlan{
				Mode: "PROVIDER_ENFORCED_LEASE", LeaseID: "lease-r0",
				LeaseUntil:          authorityTime(time.Now().UTC().Add(5 * time.Minute)),
				AuthorityWatermark:  0,
				ExecutionGeneration: 7,
				ScopedEpochs:        append([]authority.ScopedEpoch(nil), signed.proof.Scopes...),
				ProviderCapabilityRef: authoritycommit.PreparedObject{
					ID: "provider-capability-r0", Digest: strings.Repeat("c", 64), ObjectRef: "object:provider-capability-r0",
				},
			},
		},
	}
	if result := authoritycommit.Validate(requester); result.Outcome != authority.OutcomeValid {
		t.Fatalf("test admission request invalid: %+v", result)
	}
	commitRepo := magi.NewAuthorityCommitRepository(base.db, magi.AuthorityRepositoryConfig{
		Enabled: true, RecoveryTrust: signed.trust,
	})
	magi.SetExposureGateAttestationVerifierForTest(commitRepo, localGateAttestationVerifier{})
	return &journalAdmitMySQLFixture{
		authoritySQLFixture: base,
		commitRepo:          commitRepo,
		owner:               owner, request: requester, c2Request: gateVerification,
		auditRequest: authoritycommit.ReplayRequest{Original: requester, AuditVerification: auditVerification},
		bootstrapRaw: signed.bootstrapRaw, proofRaw: signed.proofRaw, proofID: signed.proofID,
		proofExpires: proofExpires,
	}
}

func TestAuthorityCommitMySQL_FirstAdmissionWritesCompleteAtomicSet(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	result := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if result.Outcome != authority.OutcomeValid || result.Code != "ADMITTED" || result.Commit == nil {
		t.Fatalf("commit result=%+v", result)
	}
	if result.ExposurePermitted || result.Commit.ExposurePermitted {
		t.Fatalf("admission granted exposure: %+v", result)
	}
	if result.Commit.AuthoritySequence != 1 || result.Commit.JournalHighWatermark != 1 || result.Commit.JournalLocalSequence != 1 {
		t.Fatalf("allocation=%+v", result.Commit)
	}
	if !containsAll(result.BlockedCapabilities, "PROVIDER_FENCE_CAPABILITY_UNVERIFIED", "INDEPENDENT_WITNESS_UNAVAILABLE") {
		t.Fatalf("blocked capabilities=%v", result.BlockedCapabilities)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)

	var journal struct {
		HighWatermark, PendingCount, ExecutionGeneration uint64
		State                                            string
	}
	if err := f.db.Raw(`SELECT high_watermark, pending_count, execution_generation, state FROM magi_authority_journal_v2
		WHERE trust_domain='tenant-a' AND journal_id='journal-r0'`).Scan(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if journal.HighWatermark != 1 || journal.PendingCount != 1 || journal.ExecutionGeneration != 7 || journal.State != "OPEN" {
		t.Fatalf("journal=%+v", journal)
	}
	counts := map[string]int64{
		"magi_authority_journal_event_v2":        1,
		"magi_authority_admission_v2":            1,
		"magi_authority_dependency_v2":           6,
		"magi_authority_provider_reservation_v2": 1,
		"magi_authority_exposure_fence_v2":       1,
		"magi_authority_pending_transition_v2":   1,
		"magi_authority_unique_binding_v2":       1,
		"magi_authority_token_consumed_v2":       1,
		"magi_authority_lineage_v2":              1,
		"magi_authority_outbox_v2":               1,
	}
	for table, want := range counts {
		var got int64
		if err := f.db.Table(table).Count(&got).Error; err != nil || got != want {
			t.Fatalf("%s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
	var admission struct {
		AuthoritySequence, JournalHighWatermark, ExecutionGeneration uint64
		ExposurePermitted                                            bool
		State                                                        string
	}
	if err := f.db.Raw(`SELECT authority_sequence, journal_high_watermark, execution_generation, exposure_permitted, state
		FROM magi_authority_admission_v2 WHERE trust_domain='tenant-a' AND commit_id=?`, result.Commit.CommitID).Scan(&admission).Error; err != nil {
		t.Fatal(err)
	}
	if admission.AuthoritySequence != 1 || admission.JournalHighWatermark != 1 || admission.ExecutionGeneration != 7 || admission.ExposurePermitted || admission.State != "ADMITTED" {
		t.Fatalf("admission=%+v", admission)
	}
	var reservation, fence, pending struct {
		State string
	}
	if err := f.db.Raw(`SELECT state FROM magi_authority_provider_reservation_v2 WHERE commit_id=?`, result.Commit.CommitID).Scan(&reservation).Error; err != nil || reservation.State != "RESERVED" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	if err := f.db.Raw(`SELECT state FROM magi_authority_exposure_fence_v2 WHERE commit_id=?`, result.Commit.CommitID).Scan(&fence).Error; err != nil || fence.State != "DECLARED_BLOCKED" {
		t.Fatalf("fence=%+v err=%v", fence, err)
	}
	if err := f.db.Raw(`SELECT state FROM magi_authority_outbox_v2 WHERE commit_id=?`, result.Commit.CommitID).Scan(&pending).Error; err != nil || pending.State != "BLOCKED" {
		t.Fatalf("outbox=%+v err=%v", pending, err)
	}
}

func containsAll(values []string, wants ...string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	for _, want := range wants {
		if !seen[want] {
			return false
		}
	}
	return true
}

func TestAuthorityCommitMySQL_SameKeyConcurrentReturnsOneCommitAndReplays(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	const callers = 8
	start := make(chan struct{})
	results := make(chan authoritycommit.Result, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			results <- f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var newCount, blockedRetryCount int
	var original *authoritycommit.AdmissionCommit
	for result := range results {
		switch result.Disposition {
		case "NEW":
			if result.Outcome != authority.OutcomeValid || result.Commit == nil || result.ExposurePermitted || result.Commit.ExposurePermitted {
				t.Fatalf("concurrent new result=%+v", result)
			}
			newCount++
		default:
			if result.Outcome != authority.OutcomeDenied || result.Code != "AUDIT_AUTHORIZATION_REQUIRED" || result.Commit != nil || result.ExposurePermitted {
				t.Fatalf("concurrent retry result=%+v", result)
			}
			blockedRetryCount++
		}
		if original == nil {
			if result.Commit == nil {
				continue
			}
			original = result.Commit
		}
	}
	if newCount != 1 || blockedRetryCount != callers-1 || original == nil {
		t.Fatalf("new=%d blocked=%d original=%+v", newCount, blockedRetryCount, original)
	}
	replay := f.commitRepo.ReplayJournalAdmissionV2(context.Background(), f.auditRequest)
	if replay.Outcome != authority.OutcomeValid || replay.Disposition != "REPLAY" || replay.Commit == nil ||
		replay.Commit.CommitID != original.CommitID || replay.Commit.AuthoritySequence != original.AuthoritySequence ||
		replay.Commit.LineageDigest != original.LineageDigest || replay.Commit.JournalHighWatermark != original.JournalHighWatermark ||
		replay.ExposurePermitted || replay.Commit.ExposurePermitted {
		t.Fatalf("audited replay=%+v original=%+v", replay, original)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	for table, want := range map[string]int64{
		"magi_authority_admission_v2":            1,
		"magi_authority_journal_event_v2":        1,
		"magi_authority_unique_binding_v2":       1,
		"magi_authority_token_consumed_v2":       1,
		"magi_authority_pending_transition_v2":   1,
		"magi_authority_provider_reservation_v2": 1,
	} {
		var got int64
		if err := f.db.Table(table).Count(&got).Error; err != nil || got != want {
			t.Fatalf("%s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
}

func TestAuthorityCommitMySQL_SameKeyDifferentDigestConflictsWithoutMutation(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	changed := f.request
	changed.CommitToken = "different-commit-token"
	changed.Admission.AdmissionInput.Digest = strings.Repeat("f", 64)
	if result := authoritycommit.Validate(changed); result.Outcome != authority.OutcomeValid {
		t.Fatalf("changed request invalid: %+v", result)
	}
	conflict := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, changed)
	if conflict.Outcome != authority.OutcomeConflict || conflict.Code != "ADMISSION_KEY_CONFLICT" || conflict.Commit != nil || conflict.ExposurePermitted {
		t.Fatalf("conflict result=%+v", conflict)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	var admissionCount int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissionCount).Error; err != nil || admissionCount != 1 {
		t.Fatalf("admission rows=%d err=%v", admissionCount, err)
	}
	var commitment struct {
		CommitID, RequestDigest string
	}
	if err := f.db.Raw(`SELECT commit_id, request_digest FROM magi_authority_admission_v2`).Scan(&commitment).Error; err != nil {
		t.Fatal(err)
	}
	if commitment.CommitID != first.Commit.CommitID || commitment.RequestDigest != first.Commit.RequestDigest {
		t.Fatalf("original admission changed: %+v", commitment)
	}
}

func TestAuthorityCommitMySQL_ReplayAfterOwnerLossReturnsOriginalWithoutExposure(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	if err := f.db.Exec(`UPDATE decision_job SET worker_id='worker-b', lease_until=CURRENT_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id='job-42'`).Error; err != nil {
		t.Fatal(err)
	}
	replay := f.commitRepo.ReplayJournalAdmissionV2(context.Background(), f.auditRequest)
	if replay.Outcome != authority.OutcomeValid || replay.Disposition != "REPLAY" || replay.Commit == nil ||
		replay.Commit.CommitID != first.Commit.CommitID || replay.Commit.AuthoritySequence != first.Commit.AuthoritySequence ||
		replay.ExposurePermitted || replay.Commit.ExposurePermitted {
		t.Fatalf("replay result=%+v original=%+v", replay, first)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	var pending uint64
	if err := f.db.Raw(`SELECT pending_count FROM magi_authority_journal_v2 WHERE journal_id='journal-r0'`).Scan(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
}

func TestAuthorityCommitMySQL_VerifyThenRevokeRejectsOldAuthority(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	if result := f.repo.VerifyProof(context.Background(), f.c2Request); result.Outcome != authority.OutcomeValid {
		t.Fatalf("pre-commit verify=%+v", result)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	if _, result := f.repo.RevokeScope(context.Background(), authorityRevocationCommand(t, f.authoritySQLFixture, "revoke-before-admission", scope, 4, 1, "withdraw")); result.Outcome != authority.OutcomeValid {
		t.Fatalf("revoke=%+v", result)
	}
	result := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if result.Outcome != authority.OutcomeRevoked || result.Code != "SCOPE_REVOKED" || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("commit after revoke=%+v", result)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 0, 1, 0)
	var admissions int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil || admissions != 0 {
		t.Fatalf("admissions=%d err=%v", admissions, err)
	}
}

func TestAuthorityCommitMySQL_GenerationChangeRejectsOldOwner(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	if err := f.db.Exec(`UPDATE decision_case SET execution_generation=8 WHERE id='case-42'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE decision_job SET execution_generation=8, worker_id='worker-b' WHERE id='job-42'`).Error; err != nil {
		t.Fatal(err)
	}
	result := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if result.Outcome != authority.OutcomeStale || result.Code != "EXECUTION_GENERATION_STALE" || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("old-generation commit=%+v", result)
	}
	assertAuthorityHeadZeroOrAbsent(t, f.authoritySQLFixture)
	for _, table := range []string{"magi_authority_journal_v2", "magi_authority_admission_v2", "magi_authority_provider_reservation_v2"} {
		var count int64
		if err := f.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s rows=%d err=%v", table, count, err)
		}
	}
}

func TestAuthorityCommitMySQL_LeaseExpiryDuringHeadWaitRejects(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain) VALUES ('tenant-a')`).Error; err != nil {
		t.Fatal(err)
	}
	holder := f.db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	var locked string
	if err := holder.Raw(`SELECT trust_domain FROM magi_authority_head WHERE trust_domain='tenant-a' FOR UPDATE`).Row().Scan(&locked); err != nil {
		_ = holder.Rollback().Error
		t.Fatal(err)
	}
	done := make(chan authoritycommit.Result, 1)
	go func() {
		done <- f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	}()
	select {
	case result := <-done:
		_ = holder.Rollback().Error
		t.Fatalf("commit did not wait for the common head: %+v", result)
	case <-time.After(150 * time.Millisecond):
	}
	if err := f.db.Exec(`UPDATE decision_job SET lease_until=CURRENT_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id='job-42'`).Error; err != nil {
		_ = holder.Rollback().Error
		t.Fatal(err)
	}
	if err := holder.Commit().Error; err != nil {
		t.Fatal(err)
	}
	var result authoritycommit.Result
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not finish after releasing head")
	}
	if result.Outcome != authority.OutcomeStale || result.Code != "OWNER_LEASE_EXPIRED" || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("expired-lease commit=%+v", result)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 0, 0, 0, 0)
	var journals int64
	if err := f.db.Table("magi_authority_journal_v2").Count(&journals).Error; err != nil || journals != 0 {
		t.Fatalf("journals=%d err=%v", journals, err)
	}
}

func TestAuthorityCommitMySQL_EveryWriteFenceRollsBackWholeCommit(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	points := []string{
		"after_journal", "after_authority_sequence", "after_admission", "after_journal_event",
		"after_dependencies", "after_reservation", "after_fence", "after_pending",
		"after_unique", "after_token", "after_lineage", "after_outbox",
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, func(got string) error {
				if got == point {
					return errors.New("injected write fault")
				}
				return nil
			})
			result := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
			magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, nil)
			if result.Outcome != authority.OutcomeUnknown || result.Code != "INJECTED_FAULT" || result.Commit != nil || result.ExposurePermitted {
				t.Fatalf("fault %s result=%+v", point, result)
			}
			for table, want := range map[string]int64{
				"magi_authority_journal_v2":              0,
				"magi_authority_journal_event_v2":        0,
				"magi_authority_admission_v2":            0,
				"magi_authority_dependency_v2":           0,
				"magi_authority_provider_reservation_v2": 0,
				"magi_authority_exposure_fence_v2":       0,
				"magi_authority_pending_transition_v2":   0,
				"magi_authority_unique_binding_v2":       0,
				"magi_authority_token_consumed_v2":       0,
				"magi_authority_lineage_v2":              0,
				"magi_authority_outbox_v2":               0,
			} {
				var got int64
				if err := f.db.Table(table).Count(&got).Error; err != nil || got != want {
					t.Fatalf("%s after %s rows=%d want=%d err=%v", table, point, got, want, err)
				}
			}
			assertAuthorityHeadZeroOrAbsent(t, f.authoritySQLFixture)
		})
	}
}

func TestAuthorityCommitMySQL_AfterCommitResponseLossReplaysOriginal(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, func(point string) error {
		if point == "after_commit" {
			return errors.New("injected response loss")
		}
		return nil
	})
	lost := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, nil)
	if lost.Outcome != authority.OutcomeUnknown || lost.Code != "COMMIT_RESPONSE_LOST" || lost.Commit != nil || lost.ExposurePermitted {
		t.Fatalf("lost response=%+v", lost)
	}
	restarted := magi.NewAuthorityCommitRepository(f.db, magi.AuthorityRepositoryConfig{Enabled: true, RecoveryTrust: f.trust})
	replay := restarted.ReplayJournalAdmissionV2(context.Background(), f.auditRequest)
	if replay.Outcome != authority.OutcomeValid || replay.Disposition != "REPLAY" || replay.Commit == nil ||
		replay.Commit.AuthoritySequence != 1 || replay.Commit.JournalHighWatermark != 1 || replay.ExposurePermitted || replay.Commit.ExposurePermitted {
		t.Fatalf("replay=%+v", replay)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	var admissions int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil || admissions != 1 {
		t.Fatalf("admissions=%d err=%v", admissions, err)
	}
}

func TestAuthorityCommitMySQL_CommitTokenCannotAuthorizeDifferentAdmission(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	second := f.request
	second.Admission.Key.ContextManifestID = "manifest-r1"
	second.Admission.Key.DeliveryID = "delivery-r1"
	second.Admission.Key.ProviderRequestID = "request-r1"
	second.Admission.HighWatermark.JournalID = "journal-r1"
	second.Admission.HighWatermark.Expected = 0
	second.Admission.HighWatermark.Next = 1
	second.Admission.PendingTransition.JournalID = "journal-r1"
	second.Admission.PendingTransition.Expected = 0
	second.Admission.PendingTransition.Next = 1
	second.Admission.ProviderReservation.RequestID = "request-r1"
	second.Admission.ProviderReservation.SessionScopeID = "session-r1"
	second.Admission.Fence.LeaseID = "lease-r1"
	if result := authoritycommit.Validate(second); result.Outcome != authority.OutcomeValid {
		t.Fatalf("second request invalid: %+v", result)
	}
	conflict := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, second)
	if conflict.Outcome != authority.OutcomeConflict || conflict.Code != "COMMIT_TOKEN_REUSED" || conflict.Commit != nil || conflict.ExposurePermitted {
		t.Fatalf("reused token result=%+v", conflict)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	var admissions, bindings, journals int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Table("magi_authority_unique_binding_v2").Count(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Table("magi_authority_journal_v2").Count(&journals).Error; err != nil {
		t.Fatal(err)
	}
	if admissions != 1 || bindings != 1 || journals != 1 {
		t.Fatalf("admissions=%d bindings=%d journals=%d", admissions, bindings, journals)
	}
}

func TestAuthorityCommitMySQL_DisabledC2FailsClosedWithoutWrites(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	disabled := magi.NewAuthorityCommitRepository(f.db, magi.AuthorityRepositoryConfig{})
	result := disabled.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if result.Outcome != authority.OutcomeDenied || result.Code != "AUTHORITY_CAPABILITY_DISABLED" || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("disabled result=%+v", result)
	}
	assertAuthorityHeadZeroOrAbsent(t, f.authoritySQLFixture)
	var admissions int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil || admissions != 0 {
		t.Fatalf("admissions=%d err=%v", admissions, err)
	}
}

func TestAuthorityCommitMySQL_ProofExpiryDuringJournalWaitRejects(t *testing.T) {
	f := newJournalAdmitMySQLWithProofTTL(t, 1500*time.Millisecond)
	if result := f.repo.VerifyProof(context.Background(), f.c2Request); result.Outcome != authority.OutcomeValid {
		t.Fatalf("initial proof verify=%+v", result)
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain) VALUES ('tenant-a')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_journal_v2
		(trust_domain, journal_id, case_id, case_round, context_manifest_id, persona_id,
		 execution_generation, high_watermark, pending_count, row_version, state)
		VALUES ('tenant-a', 'journal-r0', 'case-42', 2, 'manifest-r0', 'melchior', 7, 0, 0, 1, 'OPEN')`).Error; err != nil {
		t.Fatal(err)
	}
	holder := f.db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	var locked string
	if err := holder.Raw(`SELECT journal_id FROM magi_authority_journal_v2 WHERE trust_domain='tenant-a' AND journal_id='journal-r0' FOR UPDATE`).Row().Scan(&locked); err != nil {
		_ = holder.Rollback().Error
		t.Fatal(err)
	}
	done := make(chan authoritycommit.Result, 1)
	go func() {
		done <- f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	}()
	select {
	case result := <-done:
		_ = holder.Rollback().Error
		t.Fatalf("commit did not wait for journal lock: %+v", result)
	case <-time.After(150 * time.Millisecond):
	}
	if wait := time.Until(f.proofExpires) + 100*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if err := holder.Commit().Error; err != nil {
		t.Fatal(err)
	}
	var result authoritycommit.Result
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not finish after journal lock release")
	}
	if result.Outcome != authority.OutcomeExpired || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("expired proof commit=%+v", result)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 0, 0, 0, 0)
	var admissions, events int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil || admissions != 0 {
		t.Fatalf("admissions=%d err=%v", admissions, err)
	}
	if err := f.db.Table("magi_authority_journal_event_v2").Count(&events).Error; err != nil || events != 0 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	var journal struct{ HighWatermark, PendingCount uint64 }
	if err := f.db.Raw(`SELECT high_watermark, pending_count FROM magi_authority_journal_v2 WHERE journal_id='journal-r0'`).Scan(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if journal.HighWatermark != 0 || journal.PendingCount != 0 {
		t.Fatalf("journal changed after expired proof: %+v", journal)
	}
}

func TestAuthorityCommitMySQL_ConcurrentRevocationAndAdmissionUseOneOrder(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	start := make(chan struct{})
	commitCh := make(chan authoritycommit.Result, 1)
	revokeCh := make(chan authority.Result, 1)
	revocation := authorityRevocationCommand(t, f.authoritySQLFixture, "revoke-race",
		authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}, 4, 1, "race")
	go func() {
		<-start
		commitCh <- f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	}()
	go func() {
		<-start
		_, result := f.repo.RevokeScope(context.Background(), revocation)
		revokeCh <- result
	}()
	close(start)
	commitResult := <-commitCh
	revokeResult := <-revokeCh
	if revokeResult.Outcome != authority.OutcomeValid {
		t.Fatalf("revocation result=%+v", revokeResult)
	}
	var head struct{ LastAdmissionSequence, LastRevocationSequence uint64 }
	if err := f.db.Raw(`SELECT last_admission_sequence, last_revocation_sequence FROM magi_authority_head WHERE trust_domain='tenant-a'`).Scan(&head).Error; err != nil {
		t.Fatal(err)
	}
	var admissions int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissions).Error; err != nil {
		t.Fatal(err)
	}
	switch commitResult.Disposition {
	case "NEW":
		if commitResult.Outcome != authority.OutcomeValid || commitResult.Commit == nil || admissions != 1 ||
			head.LastAdmissionSequence == 0 || head.LastAdmissionSequence >= head.LastRevocationSequence || commitResult.ExposurePermitted {
			t.Fatalf("admission-first commit=%+v head=%+v admissions=%d", commitResult, head, admissions)
		}
	case "":
		if commitResult.Outcome != authority.OutcomeRevoked || commitmentsCount(t, f) != 0 || head.LastAdmissionSequence != 0 || head.LastRevocationSequence != 1 {
			t.Fatalf("revocation-first commit=%+v head=%+v", commitResult, head)
		}
	default:
		t.Fatalf("unexpected concurrent disposition %q: %+v", commitResult.Disposition, commitResult)
	}
}

func TestAuthorityCommitMySQL_CommitRetryDoesNotAutoGrantHistoricalRead(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	if _, result := f.repo.RevokeScope(context.Background(), authorityRevocationCommand(t, f.authoritySQLFixture,
		"revoke-before-unaudited-replay", scope, 4, 1, "withdraw")); result.Outcome != authority.OutcomeValid {
		t.Fatalf("revoke=%+v", result)
	}
	retry := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if retry.Outcome != authority.OutcomeDenied || retry.Code != "AUDIT_AUTHORIZATION_REQUIRED" || retry.Commit != nil || retry.ExposurePermitted {
		t.Fatalf("retry auto-granted historical read: %+v", retry)
	}
}

func TestAuthorityCommitMySQL_AuthorizedAuditCanReplayAfterOperationalRevocation(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	if _, result := f.repo.RevokeScope(context.Background(), authorityRevocationCommand(t, f.authoritySQLFixture,
		"revoke-before-audited-replay", scope, 4, 1, "withdraw")); result.Outcome != authority.OutcomeValid {
		t.Fatalf("revoke=%+v", result)
	}
	replay := f.commitRepo.ReplayJournalAdmissionV2(context.Background(), f.auditRequest)
	if replay.Outcome != authority.OutcomeValid || replay.Disposition != "REPLAY" || replay.Commit == nil ||
		replay.Commit.CommitID != first.Commit.CommitID || replay.Commit.AuthoritySequence != first.Commit.AuthoritySequence ||
		replay.ExposurePermitted || replay.Commit.ExposurePermitted {
		t.Fatalf("audited replay=%+v original=%+v", replay, first)
	}
}

func TestAuthorityCommitMySQL_UnauthorizedAuditCannotReadHistoricalLineage(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	unauthorized := f.auditRequest
	unauthorized.AuditVerification = authority.VerificationRequest{}
	replay := f.commitRepo.ReplayJournalAdmissionV2(context.Background(), unauthorized)
	if replay.Outcome != authority.OutcomeDenied || replay.Code != "AUDIT_AUTHORIZATION_REQUIRED" || replay.Commit != nil || replay.ExposurePermitted {
		t.Fatalf("unauthorized replay=%+v", replay)
	}
	oldOperationalProof := f.auditRequest
	oldOperationalProof.AuditVerification = f.c2Request
	replay = f.commitRepo.ReplayJournalAdmissionV2(context.Background(), oldOperationalProof)
	if replay.Outcome != authority.OutcomeDenied || replay.Code != "AUDIT_BINDING_MISMATCH" || replay.Commit != nil || replay.ExposurePermitted {
		t.Fatalf("old operational proof read lineage=%+v", replay)
	}
}

func TestAuthorityCommitMySQL_RevokedAuditProofCannotReadHistoricalLineage(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	first := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if first.Outcome != authority.OutcomeValid || first.Commit == nil {
		t.Fatalf("first result=%+v", first)
	}
	auditScope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "audit-context-scope"}
	if _, result := f.repo.RevokeScope(context.Background(), authorityRevocationCommand(t, f.authoritySQLFixture,
		"revoke-audit-read", auditScope, 2, 1, "audit access withdrawn")); result.Outcome != authority.OutcomeValid {
		t.Fatalf("revoke audit scope=%+v", result)
	}
	replay := f.commitRepo.ReplayJournalAdmissionV2(context.Background(), f.auditRequest)
	if replay.Outcome != authority.OutcomeRevoked || replay.Code != "SCOPE_REVOKED" || replay.Commit != nil || replay.ExposurePermitted {
		t.Fatalf("revoked audit replay=%+v", replay)
	}
}

func commitmentsCount(t *testing.T, f *journalAdmitMySQLFixture) int64 {
	t.Helper()
	var count int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAuthorityCommitMySQL_ExposureGateFirstCommitUsesCommonOrderWithoutPermit(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	gateRequest := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain:         f.c2Request.TrustDomain,
			ProofID:             f.c2Request.ProofID,
			Subject:             f.c2Request.Subject,
			Resource:            "gate-r0",
			Action:              f.c2Request.Action,
			Tenant:              f.c2Request.Tenant,
			Operation:           authoritycommit.JournalExposureGateV2,
			CaseID:              f.c2Request.CaseID,
			Round:               f.c2Request.Round,
			ExecutionGeneration: f.c2Request.ExecutionGeneration,
			Scopes:              f.c2Request.Scopes,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: admission.Commit.CommitID, RequestDigest: mustAdmissionRequestDigest(t, f.request),
			ValidUntil: authorityTime(time.Now().UTC().Add(10 * time.Minute)),
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID:                 "gate-r0",
			AdmissionCommitID:      admission.Commit.CommitID,
			AdmissionRequestDigest: mustAdmissionRequestDigest(t, f.request),
			FenceID:                f.request.Admission.Fence.LeaseID,
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID:       "provider-r0",
				CapabilityID:     "provider-capability-r0",
				CapabilityDigest: strings.Repeat("e", 64),
				VerificationRef: authoritycommit.PreparedObject{
					ID: "provider-capability-r0", Digest: strings.Repeat("c", 64), ObjectRef: "object:provider-capability-r0",
				},
				EnforcesFence:               true,
				SupportsLease:               true,
				EnforcesRevocationWatermark: true,
				MaxLeaseUntil:               authorityTime(time.Now().UTC().Add(10 * time.Minute)),
			},
		},
	}
	gateRequest.Witness.RequestDigest = gateRequest.Gate.AdmissionRequestDigest
	result := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, gateRequest)
	if result.Outcome != authority.OutcomeValid || result.Attempt == nil || result.Attempt.Outcome != "EXPOSURE_GATED" {
		t.Fatalf("gate result=%+v", result)
	}
	if result.ExposurePermitted || result.Attempt.ExposurePermitted {
		t.Fatalf("gate must not grant a locked permit in this slice: %+v", result)
	}
	if result.Attempt.AuthoritySequence != admission.Commit.AuthoritySequence+1 {
		t.Fatalf("gate sequence=%d admission=%d", result.Attempt.AuthoritySequence, admission.Commit.AuthoritySequence)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_ExposureGateSchemaSurvivesRereadWithoutPermit(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	gateRequest := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain: f.c2Request.TrustDomain, ProofID: f.c2Request.ProofID,
			Subject: f.c2Request.Subject, Resource: "gate-r0", Action: f.c2Request.Action,
			Tenant: f.c2Request.Tenant, Operation: authoritycommit.JournalExposureGateV2,
			CaseID: f.c2Request.CaseID, Round: f.c2Request.Round,
			ExecutionGeneration: f.c2Request.ExecutionGeneration, Scopes: f.c2Request.Scopes,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: admission.Commit.CommitID, RequestDigest: mustAdmissionRequestDigest(t, f.request),
			ValidUntil: authorityTime(time.Now().UTC().Add(10 * time.Minute)),
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID: "gate-r0", AdmissionCommitID: admission.Commit.CommitID,
			AdmissionRequestDigest: mustAdmissionRequestDigest(t, f.request),
			FenceID:                f.request.Admission.Fence.LeaseID,
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID: "provider-r0", CapabilityID: "provider-capability-r0",
				CapabilityDigest: strings.Repeat("e", 64),
				VerificationRef: authoritycommit.PreparedObject{
					ID: "provider-capability-r0", Digest: strings.Repeat("c", 64),
					ObjectRef: "object:provider-capability-r0",
				},
				EnforcesFence: true, SupportsLease: true,
				EnforcesRevocationWatermark: true,
				MaxLeaseUntil:               authorityTime(time.Now().UTC().Add(10 * time.Minute)),
			},
		},
	}
	first := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, gateRequest)
	if first.Outcome != authority.OutcomeValid || first.Attempt == nil || first.ExposurePermitted {
		t.Fatalf("first gate=%+v", first)
	}
	replayed := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, gateRequest)
	if replayed.Outcome != authority.OutcomeValid || replayed.Disposition != "REPLAY" || replayed.Attempt == nil ||
		replayed.Attempt.AuthoritySequence != first.Attempt.AuthoritySequence || replayed.ExposurePermitted {
		t.Fatalf("replayed gate=%+v first=%+v", replayed, first)
	}
	if replayed.Attempt.Outcome == "BLOCKED" {
		t.Fatal("replay changed a blocked attempt into a successful gate")
	}
	var rows int64
	if err := f.db.Table("magi_authority_exposure_gate_commit_v2").Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("gate commit rows=%d err=%v", rows, err)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_BlockedAttemptIsTerminalAndNeverRetriedSuccessfully(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain: f.c2Request.TrustDomain, ProofID: f.c2Request.ProofID,
			Subject: f.c2Request.Subject, Resource: "gate-r0", Action: f.c2Request.Action,
			Tenant: f.c2Request.Tenant, Operation: authoritycommit.JournalExposureGateV2,
			CaseID: f.c2Request.CaseID, Round: f.c2Request.Round,
			ExecutionGeneration: f.c2Request.ExecutionGeneration, Scopes: f.c2Request.Scopes,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: admission.Commit.CommitID, RequestDigest: mustAdmissionRequestDigest(t, f.request),
			ValidUntil: authorityTime(time.Now().UTC().Add(10 * time.Minute)),
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID: "gate-r0", AdmissionCommitID: admission.Commit.CommitID,
			AdmissionRequestDigest: mustAdmissionRequestDigest(t, f.request),
			FenceID:                f.request.Admission.Fence.LeaseID,
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID: "provider-r0", CapabilityID: "provider-capability-r0",
				CapabilityDigest: strings.Repeat("e", 64),
				VerificationRef: authoritycommit.PreparedObject{
					ID: "provider-capability-r0", Digest: strings.Repeat("c", 64),
					ObjectRef: "object:provider-capability-r0",
				},
				EnforcesFence: true, SupportsLease: true,
				EnforcesRevocationWatermark: false,
				MaxLeaseUntil:               authorityTime(time.Now().UTC().Add(10 * time.Minute)),
			},
		},
	}
	blocked := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if blocked.Outcome != authority.OutcomeDenied || blocked.Code != "PROVIDER_CAPABILITY_INSUFFICIENT" || blocked.ExposurePermitted {
		t.Fatalf("blocked gate=%+v", blocked)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	changed := request
	changed.Gate.ProviderCapability.EnforcesRevocationWatermark = true
	changed.Witness.RequestDigest = changed.Gate.AdmissionRequestDigest
	conflict := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, changed)
	if conflict.Outcome != authority.OutcomeConflict || conflict.Code != "EXPOSURE_GATE_ATTEMPT_CONFLICT" || conflict.ExposurePermitted {
		t.Fatalf("reused blocked attempt=%+v", conflict)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)

	fixed := changed
	fixed.AttemptID = "attempt-after-fix"
	fixed.CommitToken = "gate-token-after-fix"
	retried := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, fixed)
	if retried.Outcome != authority.OutcomeValid || retried.Disposition != "NEW" || retried.ExposurePermitted {
		t.Fatalf("new attempt after fixing capability=%+v", retried)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_ExposureGateConcurrentSameAttemptCreatesOneCommit(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	const callers = 8
	start := make(chan struct{})
	results := make(chan authoritycommit.ExposureGateResult, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			results <- f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var newCount, replayCount int
	for result := range results {
		if result.ExposurePermitted {
			t.Fatalf("concurrent gate permitted exposure: %+v", result)
		}
		if result.Disposition == "NEW" {
			newCount++
		} else if result.Disposition == "REPLAY" {
			replayCount++
		}
	}
	if newCount != 1 || replayCount != callers-1 {
		t.Fatalf("new=%d replay=%d", newCount, replayCount)
	}
	var gateCommits, gateTokens, gateLineages int64
	for table, count := range map[string]*int64{
		"magi_authority_exposure_gate_commit_v2":  &gateCommits,
		"magi_authority_exposure_gate_token_v2":   &gateTokens,
		"magi_authority_exposure_gate_lineage_v2": &gateLineages,
	} {
		if err := f.db.Table(table).Count(count).Error; err != nil || *count != 1 {
			t.Fatalf("%s=%d err=%v", table, *count, err)
		}
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_ExposureGateStaleGenerationAndOwnerFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*entity.ExecutionContext)
		code   string
	}{
		{"old-generation", func(owner *entity.ExecutionContext) { owner.ExecutionGeneration = 6 }, "EXECUTION_GENERATION_STALE"},
		{"old-worker", func(owner *entity.ExecutionContext) { owner.WorkerID = "worker-old" }, "OWNER_FENCE_STALE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newJournalAdmitMySQL(t)
			admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
			if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
				t.Fatalf("admission=%+v", admission)
			}
			request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
			owner := *f.owner
			tc.mutate(&owner)
			result := f.commitRepo.CommitJournalExposureGateV2(context.Background(), &owner, request)
			if result.Outcome != authority.OutcomeStale || result.Code != tc.code || result.ExposurePermitted {
				t.Fatalf("result=%+v want STALE/%s", result, tc.code)
			}
			assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
		})
	}
}

func TestAuthorityCommitMySQL_ExposureGateRevocationBeforeGateWins(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	revoked, result := f.repo.RevokeScope(context.Background(), authorityRevocationCommand(t, f.authoritySQLFixture, "gate-revoke", scope, 4, 1, "withdraw"))
	if result.Outcome != authority.OutcomeValid || !revoked.Revoked {
		t.Fatalf("revoke=%+v result=%+v", revoked, result)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	gate := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if gate.Outcome != authority.OutcomeRevoked || gate.ExposurePermitted {
		t.Fatalf("gate after revocation=%+v", gate)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 2, 0)
}

func TestAuthorityCommitMySQL_ExposureGateEveryWriteFaultRollsBack(t *testing.T) {
	cases := []struct {
		point string
	}{
		{"gate_before_head"}, {"gate_after_head"}, {"gate_after_admission"}, {"gate_after_sequence"},
		{"gate_write_attempt"}, {"gate_write_binding"}, {"gate_write_token"},
		{"gate_write_lineage"}, {"gate_write_attestation"}, {"gate_after_commit"},
	}
	for _, tc := range cases {
		t.Run(tc.point, func(t *testing.T) {
			f := newJournalAdmitMySQL(t)
			admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
			if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
				t.Fatalf("admission=%+v", admission)
			}
			request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
			magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, func(point string) error {
				if point == tc.point {
					return errors.New("injected gate fault")
				}
				return nil
			})
			result := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
			magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, nil)
			if tc.point == "gate_after_commit" {
				if result.Outcome != authority.OutcomeUnknown || result.Code != "INJECTED_FAULT_AFTER_COMMIT" {
					t.Fatalf("post-commit result=%+v", result)
				}
				assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
				return
			}
			if result.Outcome != authority.OutcomeUnknown || result.Code != "INJECTED_FAULT" {
				t.Fatalf("fault result=%+v", result)
			}
			assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
			for _, table := range []string{
				"magi_authority_exposure_gate_attempt_v2", "magi_authority_exposure_gate_commit_v2",
				"magi_authority_exposure_gate_token_v2", "magi_authority_exposure_gate_lineage_v2",
				"magi_authority_exposure_gate_attestation_v2",
			} {
				var count int64
				if err := f.db.Table(table).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("%s rows=%d err=%v", table, count, err)
				}
			}
			var fenceState string
			if err := f.db.Raw(`SELECT state FROM magi_authority_exposure_fence_v2 WHERE commit_id=?`, admission.Commit.CommitID).Scan(&fenceState).Error; err != nil || fenceState != "DECLARED_BLOCKED" {
				t.Fatalf("fence=%q err=%v", fenceState, err)
			}
		})
	}
}

func newValidExposureGateRequest(t *testing.T, f *journalAdmitMySQLFixture, admissionCommitID string) authoritycommit.ExposureGateRequest {
	t.Helper()
	request := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain: f.c2Request.TrustDomain, ProofID: f.c2Request.ProofID,
			Subject: f.c2Request.Subject, Resource: "gate-r0", Action: f.c2Request.Action,
			Tenant: f.c2Request.Tenant, Operation: authoritycommit.JournalExposureGateV2,
			CaseID: f.c2Request.CaseID, Round: f.c2Request.Round,
			ExecutionGeneration: f.c2Request.ExecutionGeneration, Scopes: f.c2Request.Scopes,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: admissionCommitID, RequestDigest: mustAdmissionRequestDigest(t, f.request),
			ValidUntil: authorityTime(time.Now().UTC().Add(10 * time.Minute)),
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID: "gate-r0", AdmissionCommitID: admissionCommitID,
			AdmissionRequestDigest: mustAdmissionRequestDigest(t, f.request),
			FenceID:                f.request.Admission.Fence.LeaseID,
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID: "provider-r0", CapabilityID: "provider-capability-r0",
				CapabilityDigest: strings.Repeat("e", 64),
				VerificationRef: authoritycommit.PreparedObject{
					ID: "provider-capability-r0", Digest: strings.Repeat("c", 64),
					ObjectRef: "object:provider-capability-r0",
				},
				EnforcesFence: true, SupportsLease: true, EnforcesRevocationWatermark: true,
				MaxLeaseUntil: authorityTime(time.Now().UTC().Add(10 * time.Minute)),
			},
		},
	}
	digest, err := exposureGateDigestWithPlaceholderWitness(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Witness.RequestDigest = digest
	return request
}

func exposureGateDigestWithPlaceholderWitness(request authoritycommit.ExposureGateRequest) (string, error) {
	return request.Gate.AdmissionRequestDigest, nil
}

func TestAuthorityCommitMySQL_BlockedAttemptReplayIsTerminalAndFenceStaysBlocked(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	request.Witness = authoritycommit.WitnessEvidence{}
	first := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if first.Outcome != authority.OutcomeDenied || first.Code != "INDEPENDENT_WITNESS_UNAVAILABLE" ||
		first.Attempt == nil || first.Attempt.Outcome != "BLOCKED" || first.Attempt.AuthoritySequence != 0 || first.ExposurePermitted {
		t.Fatalf("blocked first=%+v", first)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
	var fenceState string
	if err := f.db.Raw(`SELECT state FROM magi_authority_exposure_fence_v2 WHERE commit_id=?`, admission.Commit.CommitID).Scan(&fenceState).Error; err != nil || fenceState != "DECLARED_BLOCKED" {
		t.Fatalf("fence=%q err=%v", fenceState, err)
	}
	replayed := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if replayed.Disposition != "REPLAY" || replayed.Attempt == nil ||
		replayed.Attempt.AttemptID != first.Attempt.AttemptID || replayed.Attempt.Outcome != "BLOCKED" ||
		replayed.ExposurePermitted {
		t.Fatalf("blocked replay=%+v first=%+v", replayed, first)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)

	// Changing the same attempt to a currently eligible body must not append
	// success or mutate the original terminal record.
	eligible := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	eligible.AttemptID = first.Attempt.AttemptID
	eligible.CommitToken = first.Attempt.AttemptID
	conflict := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, eligible)
	if conflict.Outcome != authority.OutcomeConflict || conflict.Code != "EXPOSURE_GATE_ATTEMPT_CONFLICT" || conflict.ExposurePermitted {
		t.Fatalf("attempt resurrection=%+v", conflict)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)

	// A new attempt identity may be evaluated, but never turns the old one into
	// success and never creates a consumable permit.
	eligible.AttemptID = "attempt-retry"
	eligible.CommitToken = "gate-token-retry"
	eligible.Witness.RequestDigest = eligible.Gate.AdmissionRequestDigest
	retried := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, eligible)
	if retried.Outcome != authority.OutcomeValid || retried.Disposition != "NEW" || retried.ExposurePermitted {
		t.Fatalf("new attempt=%+v", retried)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_ExposureGateConcurrentRevocationUsesOneOrder(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admission := f.commitRepo.CommitJournalAdmissionV2(ctx, f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	blocker := f.db.WithContext(ctx).Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var sequence uint64
	if err := blocker.Raw(`SELECT last_sequence FROM magi_authority_head WHERE trust_domain='tenant-a' FOR UPDATE`).Row().Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	gateDone := make(chan authoritycommit.ExposureGateResult, 1)
	revokeDone := make(chan authority.Result, 1)
	go func() { gateDone <- f.commitRepo.CommitJournalExposureGateV2(ctx, f.owner, request) }()
	waitForAuthorityHeadWaiter(t, f.authoritySQLFixture, 1)
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-gateDone:
		if result.Outcome != authority.OutcomeValid || result.ExposurePermitted || result.Attempt == nil || result.Attempt.AuthoritySequence != 2 {
			t.Fatalf("gate before revocation=%+v", result)
		}
		gateDone <- result
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	scope := authority.ScopeKey{Kind: authority.ScopeContext, ScopeID: "context-scope"}
	revocation := authorityRevocationCommand(t, f.authoritySQLFixture, "revoke-after-gate", scope, 4, 1, "withdraw after gate")
	go func() {
		_, result := f.repo.RevokeScope(ctx, revocation)
		revokeDone <- result
	}()
	select {
	case result := <-revokeDone:
		if result.Outcome != authority.OutcomeValid {
			t.Fatalf("revocation=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 3, 1, 3, 2)
	var fenceState string
	if err := f.db.Raw(`SELECT state FROM magi_authority_exposure_fence_v2 WHERE commit_id=?`, admission.Commit.CommitID).Scan(&fenceState).Error; err != nil || fenceState != "DECLARED_BLOCKED" {
		t.Fatalf("fence after revocation=%q err=%v", fenceState, err)
	}
}

func waitForAuthorityHeadWaiter(t *testing.T, f *authoritySQLFixture, minimum int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int64
		err := f.db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w
			JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE
			WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='magi_authority_head'`).Scan(&waiting).Error
		if err != nil {
			t.Fatal(err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("authority head waiters=%d, want at least %d", waiting, minimum)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustAdmissionRequestDigest(t *testing.T, request authoritycommit.AdmissionRequest) string {
	t.Helper()
	digest, err := authoritycommit.RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestAuthorityCommitMySQL_ExposureGateRejectsCapabilityLeaseShorterThanFence(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	request.Gate.ProviderCapability.MaxLeaseUntil = authorityTime(time.Now().UTC().Add(time.Minute))
	request.Witness.ValidUntil = request.Gate.ProviderCapability.MaxLeaseUntil
	request.Witness.RequestDigest = request.Gate.AdmissionRequestDigest
	result := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if result.Outcome != authority.OutcomeBlocked || result.Code != "PROVIDER_CAPABILITY_LEASE_SHORTER_THAN_FENCE" || result.ExposurePermitted {
		t.Fatalf("short capability lease=%+v", result)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
}

func TestAuthorityCommitMySQL_ExposureGateCrossCaseAdmissionBindingRejected(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	request.Verification.CaseID = "case-other"
	result := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_BINDING_MISMATCH" || result.ExposurePermitted {
		t.Fatalf("cross-case binding=%+v", result)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 1, 1, 0, 0)
}

func TestAuthorityCommitMySQL_ExposureGateTokenCannotBeReusedAcrossAttempts(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	first := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if first.Outcome != authority.OutcomeValid || first.Disposition != "NEW" {
		t.Fatalf("first=%+v", first)
	}
	second := request
	second.AttemptID = "attempt-token-reuse"
	second.Gate.GateID = "gate-token-reuse"
	second.ExactTargetID = second.Gate.GateID
	second.Verification.Resource = second.Gate.GateID
	secondReport := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, second)
	if secondReport.Outcome != authority.OutcomeConflict || secondReport.Code != "COMMIT_TOKEN_REUSED" || secondReport.ExposurePermitted {
		t.Fatalf("token reuse=%+v", secondReport)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_ExposureGateAfterCommitReplyLossReturnsOriginalLineage(t *testing.T) {
	f := newJournalAdmitMySQL(t)
	admission := f.commitRepo.CommitJournalAdmissionV2(context.Background(), f.owner, f.request)
	if admission.Outcome != authority.OutcomeValid || admission.Commit == nil {
		t.Fatalf("admission=%+v", admission)
	}
	request := newValidExposureGateRequest(t, f, admission.Commit.CommitID)
	magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, func(point string) error {
		if point == "gate_after_commit" {
			return errors.New("lost gate reply")
		}
		return nil
	})
	lost := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if lost.Outcome != authority.OutcomeUnknown || lost.Code != "INJECTED_FAULT_AFTER_COMMIT" {
		t.Fatalf("lost reply=%+v", lost)
	}
	magi.SetAuthorityCommitFaultHookForTest(f.commitRepo, nil)
	replayed := f.commitRepo.CommitJournalExposureGateV2(context.Background(), f.owner, request)
	if replayed.Outcome != authority.OutcomeValid || replayed.Disposition != "REPLAY" || replayed.Attempt == nil ||
		replayed.Attempt.AuthoritySequence != admission.Commit.AuthoritySequence+1 || replayed.ExposurePermitted {
		t.Fatalf("replayed=%+v", replayed)
	}
	var gateCommits int64
	if err := f.db.Table("magi_authority_exposure_gate_commit_v2").Count(&gateCommits).Error; err != nil || gateCommits != 1 {
		t.Fatalf("gate commits=%d err=%v", gateCommits, err)
	}
	assertAuthorityHead(t, f.authoritySQLFixture, 2, 1, 0, 2)
}

func TestAuthorityCommitMySQL_S34UpgradesExistingS33RowsWithoutMutatingThem(t *testing.T) {
	f := newAuthorityMySQL(t)
	applyAuthorityMigration(t, f, "../../docker/atlas/migrations/magi_s33_authority_commit.sql")
	if err := f.db.Exec(`INSERT INTO magi_authority_head (trust_domain,last_sequence,last_admission_sequence) VALUES ('tenant-a',1,1)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_journal_v2
		(trust_domain,journal_id,case_id,case_round,context_manifest_id,persona_id,execution_generation,high_watermark,pending_count,state)
		VALUES ('tenant-a','journal-r0','case-42',2,'manifest-r0','melchior',7,1,1,'OPEN')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO magi_authority_admission_v2
		(trust_domain,commit_id,unique_key,case_id,case_round,context_manifest_id,persona_id,delivery_id,provider_request_id,journal_id,request_digest,exact_target_id,commit_token,authority_sequence,journal_high_watermark,execution_generation,proof_id,admission_digest,admission_object_ref,raw_request,owner_job_id,owner_worker_id,owner_lease_until,state,exposure_permitted)
		VALUES ('tenant-a','ajc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',REPEAT('a',64),'case-42',2,'manifest-r0','melchior','delivery-r0','request-r0','journal-r0',REPEAT('b',64),'admission-r0','commit-r0',1,1,7,'aprf_0000000000000000000000000000000000000000000000000000000000000000',REPEAT('c',64),'object:admission-r0','{}','job-42','worker-a',CURRENT_TIMESTAMP(6),'ADMITTED',0)`).Error; err != nil {
		t.Fatal(err)
	}
	applyAuthorityMigration(t, f, "../../docker/atlas/migrations/magi_s34_authority_exposure_gate.sql")
	var admissionCount int64
	if err := f.db.Table("magi_authority_admission_v2").Count(&admissionCount).Error; err != nil || admissionCount != 1 {
		t.Fatalf("existing admission count=%d err=%v", admissionCount, err)
	}
	if f.db.Migrator().HasTable("magi_provider_settlement_v2") {
		t.Fatal("S34 must not add settlement")
	}
}
