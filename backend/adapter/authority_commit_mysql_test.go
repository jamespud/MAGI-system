package magi_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	gosqlmysql "github.com/go-sql-driver/mysql"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/authority"
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
	raw, err := os.ReadFile("../../docker/atlas/migrations/magi_s33_authority_commit.sql")
	if err != nil {
		t.Fatalf("read S33 migration: %v", err)
	}
	for _, stmt := range strings.Split(string(raw), ";") {
		var lines []string
		for _, line := range strings.Split(stmt, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines = append(lines, line)
			}
		}
		stmt = strings.TrimSpace(strings.Join(lines, "\n"))
		if stmt != "" {
			if err := f.db.Exec(stmt).Error; err != nil {
				t.Fatalf("apply S33: %v\n%s", err, stmt)
			}
		}
	}
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
	assertAuthorityHead(t, f, 0, 0)
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

func assertAuthorityHead(t *testing.T, f *authoritySQLFixture, sequence, revocation uint64) {
	t.Helper()
	var h struct{ LastSequence, LastAdmissionSequence, LastRevocationSequence, LastGateSequence, LastSettlementSequence uint64 }
	result := f.db.Raw(`SELECT last_sequence, last_admission_sequence, last_revocation_sequence, last_gate_sequence, last_settlement_sequence FROM magi_authority_head WHERE trust_domain='tenant-a'`).Scan(&h)
	if result.Error != nil || result.RowsAffected != 1 || h.LastSequence != sequence || h.LastRevocationSequence != revocation || h.LastAdmissionSequence != 0 || h.LastGateSequence != 0 || h.LastSettlementSequence != 0 {
		t.Fatalf("head=%+v rows=%d err=%v want sequence=%d revocation=%d", h, result.RowsAffected, result.Error, sequence, revocation)
	}
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
	assertAuthorityHead(t, f, 1, 1)
	replay, result := f.repo.RevokeScope(ctx, revocation)
	if result.Outcome != authority.OutcomeValid || replay != first {
		t.Fatalf("revoke replay=%+v result=%+v", replay, result)
	}
	assertAuthorityHead(t, f, 1, 1)
	replacement := authorityCredential(11, 2, "cred-governance", "governance", "credential-scope", f.base.now.Add(-time.Hour), f.base.now.Add(2*time.Hour))
	rotation := authorityRotationCommand(t, f, "ordered-rotate", f.base.governance.Ref, replacement, 5, 1, "rotate")
	rotated, result := f.repo.RotateCredential(ctx, rotation)
	if result.Outcome != authority.OutcomeValid || rotated.Ref != replacement.Ref {
		t.Fatalf("rotate=%+v result=%+v", rotated, result)
	}
	assertAuthorityHead(t, f, 2, 2)
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
	assertAuthorityHead(t, f, 2, 2)
	changed := authorityRotationCommand(t, f, "ordered-rotate", f.base.governance.Ref, replacement, 5, 1, "changed")
	if _, result := restarted.RotateCredential(ctx, changed); result.Outcome != authority.OutcomeConflict {
		t.Fatal(result)
	}
	assertAuthorityHead(t, f, 2, 2)
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
	assertAuthorityHead(t, f, 0, 0)
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
	assertAuthorityHead(t, f, 1, 1)
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
	assertAuthorityHead(t, f, ^uint64(0), 0)
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
