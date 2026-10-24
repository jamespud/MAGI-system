package magi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/inputartifact"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/readsetjournal"
	"gorm.io/gorm"
)

func migrateReadSetJournalMySQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	// This suite owns these tables and starts from a clean, migration-produced
	// shape. Do not AutoMigrate here: the real SQL migration is the acceptance
	// artifact under test.
	for _, table := range []string{
		"readset_journal_edge", "readset_journal_node", "readset_journal_delivery",
		"readset_journal_event", "readset_journal",
	} {
		if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "docker", "atlas", "migrations", "magi_s31_readset_journal.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	for _, statement := range strings.Split(strings.Join(kept, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("apply migration statement:\n%s\nerror: %v", statement, err)
		}
	}

	t.Cleanup(func() {
		// Exercise the documented rollback order. Foreign keys make a wrong
		// order observable here rather than silently leaving tables behind.
		for _, table := range []string{
			"readset_journal_edge", "readset_journal_node", "readset_journal_delivery",
			"readset_journal_event", "readset_journal",
		} {
			if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
				t.Errorf("rollback drop %s: %v", table, err)
			}
		}
	})
}

func readSetJournalIDForCase(t *testing.T, db *gorm.DB, caseID string) string {
	t.Helper()
	var journalID string
	if err := db.Table("readset_journal").Select("id").Where("case_id = ?", caseID).Scan(&journalID).Error; err != nil {
		t.Fatal(err)
	}
	if journalID == "" {
		t.Fatal("journal id not found")
	}
	return journalID
}

func newReadSetJournalRepo(db *gorm.DB, enabled bool) *magi.ReadSetJournalRepository {
	cfg := readsetjournal.DefaultConfig()
	cfg.Enabled = enabled
	return magi.NewReadSetJournalRepository(db, cfg)
}

func journalDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func journalRegistration(owner *entity.ExecutionContext, deliveryID string) readsetjournal.RegisterRequest {
	caseID := owner.CaseID
	source := inputartifact.SourceNode{
		NodeID:           "node-" + journalDigest(caseID)[:20],
		SourceEntryID:    "source-" + journalDigest(caseID)[:20],
		SourceVersion:    "1",
		RawContentDigest: journalDigest("source-content:" + caseID),
		Kind:             "RAW",
		SourceRef: inputartifact.ExternalRef{
			Namespace: "test", ObjectID: "source-" + journalDigest(caseID)[:20], Version: "1",
			DigestAlg: "sha256", ContentDigest: journalDigest("source-content:" + caseID),
		},
		Validity: inputartifact.Validity{NotBefore: "2026-10-24T00:00:00.000Z"},
		DerivationMethodRef: inputartifact.ExternalRef{
			Namespace: "test", ObjectID: "raw-method", Version: "1",
			DigestAlg: "sha256", ContentDigest: journalDigest("raw-method"),
		},
		ParentNodeIDs: []string{},
	}
	key := readsetjournal.JournalKey{
		CaseID: caseID, Round: 1,
		ContextManifestID: "ctxm_" + journalDigest("manifest:"+caseID),
		PersonaID:         "melchior",
	}
	return readsetjournal.RegisterRequest{
		Owner: readsetjournal.OwnerFence{
			CaseID: caseID, JobID: owner.JobID, WorkerID: owner.WorkerID,
			ExecutionGeneration: owner.ExecutionGeneration,
		},
		Event: readsetjournal.RegistrationPlan{
			EventVersion: readsetjournal.EventVersion, JournalKey: key,
			ExecutionGeneration: owner.ExecutionGeneration,
			DeliveryID:          deliveryID, ProviderRequestID: "provider-" + deliveryID,
			Kind: "RETRIEVAL", Phase: "INITIAL_REVIEW", Source: source,
			ParentNodeIDs: []string{},
			PermissionRef: inputartifact.ExternalRef{
				Namespace: "test", ObjectID: "permission-1", Version: "1",
				DigestAlg: "sha256", ContentDigest: journalDigest("permission"),
			},
			Producer: inputartifact.SubjectRef{
				SubjectID: "producer-1", IdentitySourceID: "identity-root-1", IdentitySourceVersion: "1",
			},
			TargetPersona: "melchior",
			AdmissionScope: readsetjournal.AdmissionScope{
				AdmissionScopeID: "admission-" + journalDigest(caseID)[:20], SourceDependencyIndexVersion: "index-1",
				ContextRevocationScopeID: "context-scope-1", ContextRevocationEpoch: 0,
				AuthorityScopeID: "authority-scope-1", AuthorityEpoch: 0,
				CredentialScopeID: "credential-scope-1", CredentialEpoch: 0,
			},
		},
		Closure: []inputartifact.SourceNode{source},
	}
}

func TestReadSetJournal_ConcurrentAppendAndContinuousPrefixMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-concurrent")
	repo := newReadSetJournalRepo(db, true)

	const callers = 24
	results := make(chan magi.ReadSetJournalRegistration, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := journalRegistration(owner, fmt.Sprintf("delivery-%02d", i))
			result, err := repo.Register(ctx, owner, req)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent registration: %v", err)
	}
	seen := make([]int64, 0, callers)
	for result := range results {
		if !result.Created || result.Event.RegistrationState != readsetjournal.RegistrationRegistered || result.Event.ProviderState != readsetjournal.ProviderPending {
			t.Fatalf("result=%+v", result)
		}
		seen = append(seen, result.Sequence)
	}
	slices.Sort(seen)
	for i, sequence := range seen {
		if sequence != int64(i+1) {
			t.Fatalf("sequences=%v, want continuous 1..%d", seen, callers)
		}
	}
	view, err := repo.ReadJournal(ctx, owner, journalRegistration(owner, "ignored").Event.JournalKey, reqScope(journalRegistration(owner, "ignored")))
	if err != nil {
		t.Fatal(err)
	}
	if view.HighWatermark != callers || len(view.Events) != callers || view.PendingCount != callers {
		t.Fatalf("view hwm=%d events=%d pending=%d", view.HighWatermark, len(view.Events), view.PendingCount)
	}
	for i, event := range view.Events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("event[%d] sequence=%d", i, event.Sequence)
		}
		if _, err := readsetjournal.AdoptRegistrationEvent(event.PayloadBytes); err != nil {
			t.Fatalf("stored event %d is not exact canonical bytes: %v", event.Sequence, err)
		}
	}
}

func reqScope(req readsetjournal.RegisterRequest) readsetjournal.AdmissionScope {
	return req.Event.AdmissionScope
}

func TestReadSetJournal_DuplicateSameBytesAndConflictMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-duplicate")
	repo := newReadSetJournalRepo(db, true)
	req := journalRegistration(owner, "delivery-duplicate")

	first, err := repo.Register(ctx, owner, req)
	if err != nil || !first.Created || first.Sequence != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := repo.Register(ctx, owner, req)
	if err != nil || replay.Created || replay.Sequence != first.Sequence || replay.Event.PayloadDigest != first.Event.PayloadDigest {
		t.Fatalf("same-ID/same-bytes replay=%+v err=%v", replay, err)
	}
	conflict := req
	conflict.Event.Kind = "CACHE_REPLAY"
	if _, err := repo.Register(ctx, owner, conflict); !errors.Is(err, readsetjournal.ErrDeliveryConflict) {
		t.Fatalf("same-ID/different-bytes error=%v", err)
	}
	scopeConflict := journalRegistration(owner, "delivery-scope-conflict")
	scopeConflict.Event.AdmissionScope.ContextRevocationEpoch++
	if _, err := repo.Register(ctx, owner, scopeConflict); !errors.Is(err, readsetjournal.ErrJournalScope) {
		t.Fatalf("changed admission scope error=%v", err)
	}
	view, err := repo.ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req))
	if err != nil {
		t.Fatal(err)
	}
	if view.HighWatermark != 1 || view.PendingCount != 1 || len(view.Events) != 1 {
		t.Fatalf("conflicting append changed journal: %+v", view)
	}
}

func TestReadSetJournal_OwnerABAAndGenerationChangeMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	const worker = "journal-same-worker"
	first, owner1 := claimArtifactOwner(t, jobs, job, worker)
	repo := newReadSetJournalRepo(db, true)
	if _, err := repo.Register(ctx, owner1, journalRegistration(owner1, "delivery-gen-1")); err != nil {
		t.Fatal(err)
	}

	requeueArtifactOwner(t, jobs, first, worker)
	second, owner2 := claimArtifactOwner(t, jobs, job, worker)
	if owner2.ExecutionGeneration != owner1.ExecutionGeneration+1 {
		t.Fatalf("generation=%d after %d", owner2.ExecutionGeneration, owner1.ExecutionGeneration)
	}
	staleReq := journalRegistration(owner1, "delivery-stale")
	staleReq.Owner.ExecutionGeneration = owner1.ExecutionGeneration
	if _, err := repo.Register(ctx, owner1, staleReq); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("stale generation registration error=%v", err)
	}
	if _, err := repo.Register(ctx, owner2, journalRegistration(owner2, "delivery-gen-2")); !errors.Is(err, readsetjournal.ErrOwnerFence) {
		t.Fatalf("cross-generation append error=%v, want ErrOwnerFence", err)
	}

	requeueArtifactOwner(t, jobs, second, worker)
	_, owner3 := claimArtifactOwner(t, jobs, job, worker)
	staleUnknown := reqScope(journalRegistration(owner1, "delivery-gen-1"))
	if _, err := repo.MarkProviderUnknown(ctx, owner1, journalRegistration(owner1, "delivery-gen-1").Event.JournalKey, staleUnknown, "delivery-gen-1", "timeout"); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("stale generation unknown settlement error=%v", err)
	}
	view, err := repo.ReadJournal(ctx, owner3, journalRegistration(owner1, "delivery-gen-1").Event.JournalKey, staleUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if view.HighWatermark != 1 || len(view.Events) != 1 {
		t.Fatalf("cross-generation append changed journal: %+v", view)
	}
}

func TestReadSetJournal_LockAfterWaitRevalidatesLeaseMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-lock-expiry")
	repo := newReadSetJournalRepo(db, true)

	blocker := openA2AMySQL(t).Begin()
	defer blocker.Rollback()
	var id string
	if err := blocker.Raw("SELECT id FROM decision_job WHERE id = ? FOR UPDATE", owner.JobID).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	name := "readset-journal:lease-expiry"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "readset-expiry" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	waitCtx, cancel := context.WithTimeout(context.WithValue(ctx, t4ContextKey{}, "readset-expiry"), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := repo.Register(waitCtx, owner, journalRegistration(owner, "delivery-expiry"))
		done <- err
	}()
	select {
	case <-entered:
	case <-waitCtx.Done():
		t.Fatal("registration never reached the owner lock")
	}
	if err := blocker.Exec("UPDATE decision_job SET lease_until = CURRENT_TIMESTAMP(6) WHERE id = ?", owner.JobID).Error; err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-done; !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("expired-after-wait error=%v", err)
	}
	var count int64
	if err := db.Table("readset_journal_event").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("event count=%d err=%v", count, err)
	}
}

func TestReadSetJournal_UnknownRestartReadbackAndPostRevocationMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	first, owner1 := claimArtifactOwner(t, jobs, job, "journal-restart")
	repo := newReadSetJournalRepo(db, true)
	req := journalRegistration(owner1, "delivery-unknown")
	registered, err := repo.Register(ctx, owner1, req)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := repo.MarkProviderUnknown(ctx, owner1, req.Event.JournalKey, reqScope(req), req.Event.DeliveryID, "provider timeout; no automatic resend")
	if err != nil {
		t.Fatal(err)
	}
	if unknown.ProviderState != readsetjournal.ProviderUnknown || unknown.Sequence != registered.Sequence {
		t.Fatalf("unknown=%+v", unknown)
	}
	replay, err := repo.Register(ctx, owner1, req)
	if err != nil || replay.Created || replay.Sequence != registered.Sequence || replay.Event.ProviderState != readsetjournal.ProviderUnknown {
		t.Fatalf("same-owner unknown replay=%+v err=%v", replay, err)
	}

	requeueArtifactOwner(t, jobs, first, owner1.WorkerID)
	_, owner2 := claimArtifactOwner(t, jobs, job, owner1.WorkerID)
	view, err := repo.ReadJournal(ctx, owner2, req.Event.JournalKey, reqScope(req))
	if err != nil {
		t.Fatalf("restart readback: %v", err)
	}
	if view.HighWatermark != 1 || len(view.Events) != 1 {
		t.Fatalf("restart view=%+v", view)
	}
	stored, err := readsetjournal.DecodeRegistrationEvent(view.Events[0].PayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeliveryID != req.Event.DeliveryID || !slices.Equal(stored.ParentNodeIDs, req.Event.ParentNodeIDs) {
		t.Fatalf("stored payload changed: %+v", stored)
	}

	reconciled, err := repo.ReconcileDelivery(ctx, owner2, req.Event.JournalKey, reqScope(req), req.Event.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.ProviderState != readsetjournal.ProviderUnknown {
		t.Fatalf("reconciled state=%s", reconciled.ProviderState)
	}
	unverifiable, err := repo.MarkProviderUnverifiable(ctx, owner2, req.Event.JournalKey, reqScope(req), req.Event.DeliveryID, "no independent provider/session source")
	if err != nil || unverifiable.ProviderState != readsetjournal.ProviderUnverifiable {
		t.Fatalf("unverifiable=%+v err=%v", unverifiable, err)
	}
	var binding struct {
		Generation int64
		Worker     string
	}
	if err := db.Raw(`SELECT last_execution_generation AS generation, last_owner_worker_id AS worker
		FROM readset_journal_delivery WHERE journal_id = ? AND delivery_id = ?`,
		readSetJournalIDForCase(t, db, owner2.CaseID), req.Event.DeliveryID).Scan(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.Generation != owner2.ExecutionGeneration || binding.Worker != owner2.WorkerID {
		t.Fatalf("provider transition owner binding=%+v, want transition generation=%d worker=%s", binding, owner2.ExecutionGeneration, owner2.WorkerID)
	}
	// Recovery is reconciliation-only. Reusing the old canonical request under a
	// different ExecutionGeneration would be a different byte payload and must
	// not be silently rebound; the new owner inspects the original delivery.
	view, err = repo.ReadJournal(ctx, owner2, req.Event.JournalKey, reqScope(req))
	if err != nil || view.PendingCount != 1 || view.Events[0].ProviderState != readsetjournal.ProviderUnverifiable {
		t.Fatalf("unverified retained view=%+v err=%v", view, err)
	}
	validatorDeliveries, err := view.ReadSetDeliveries()
	if err != nil {
		t.Fatalf("adapt journal to ReadSet validator: %v", err)
	}
	if len(validatorDeliveries) != 0 {
		t.Fatalf("unknown registration was promoted into validator deliveries: %+v", validatorDeliveries)
	}

	foreign := req.Event.JournalKey
	foreign.CaseID = "case-foreign-journal"
	if _, err := repo.ReadJournal(ctx, owner2, foreign, reqScope(req)); !errors.Is(err, readsetjournal.ErrOwnerFence) {
		t.Fatalf("cross-case read error=%v", err)
	}

	revoked := reqScope(req)
	revoked.ContextRevocationEpoch++
	if _, err := repo.ReadJournal(ctx, owner2, req.Event.JournalKey, revoked); !errors.Is(err, readsetjournal.ErrRevoked) {
		t.Fatalf("post-revocation read error=%v", err)
	}
	if _, err := repo.MarkProviderUnknown(ctx, owner2, req.Event.JournalKey, revoked, req.Event.DeliveryID, "late"); !errors.Is(err, readsetjournal.ErrRevoked) {
		t.Fatalf("post-revocation settlement error=%v", err)
	}
}

func TestReadSetJournal_AppendBeforeExposureFaultCutAndResponseLossMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-reply-loss")
	req := journalRegistration(owner, "delivery-reply-loss")

	t.Run("failure_before_commit_leaves_no_exposure_record", func(t *testing.T) {
		injected := errors.New("injected delivery insert failure")
		name := "readset-journal:delivery-insert-failure"
		if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "readset_journal_delivery" {
				tx.AddError(injected)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Create().Remove(name)
		if _, err := newReadSetJournalRepo(db, true).Register(ctx, owner, req); !errors.Is(err, injected) {
			t.Fatalf("faulted registration error=%v", err)
		}
	})
	view, err := newReadSetJournalRepo(db, true).ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req))
	if !errors.Is(err, readsetjournal.ErrNotFound) || view.HighWatermark != 0 || len(view.Events) != 0 {
		t.Fatalf("failed transaction exposed partial registration: view=%+v err=%v", view, err)
	}

	// The first commit succeeds but its reply is lost. A retry with the exact
	// same ID/bytes must recover the original sequence without another append.
	faultDB, pool := t4FaultDB(t, db, false, nil)
	result, err := newReadSetJournalRepo(faultDB, true).Register(ctx, owner, req)
	if err == nil || result.Sequence != 0 || !pool.lost.Load() {
		t.Fatalf("reply-loss result=%+v err=%v lost=%v", result, err, pool.lost.Load())
	}
	// The durable append exists even though the caller did not receive a
	// success reply. No provider call exists in this slice; the assertion marks
	// the exact crash cut that a future controlled injector must recover.
	committed, err := newReadSetJournalRepo(db, true).ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req))
	if err != nil || committed.HighWatermark != 1 || len(committed.Events) != 1 ||
		committed.Events[0].ProviderState != readsetjournal.ProviderPending {
		t.Fatalf("append-before-exposure cut view=%+v err=%v", committed, err)
	}
	replay, err := newReadSetJournalRepo(db, true).Register(ctx, owner, req)
	if err != nil || replay.Created || replay.ExposurePermitted || !replay.RequiresReconciliation ||
		replay.Sequence != 1 || replay.Event.ProviderState != readsetjournal.ProviderPending {
		t.Fatalf("reply-loss reconciliation replay=%+v err=%v", replay, err)
	}
	view, err = newReadSetJournalRepo(db, true).ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req))
	if err != nil || view.HighWatermark != 1 || len(view.Events) != 1 {
		t.Fatalf("reply-loss view=%+v err=%v", view, err)
	}
}

func TestReadSetJournal_DAGReverseIndexAndOrphanRejectionMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-dag")
	repo := newReadSetJournalRepo(db, true)

	parentReq := journalRegistration(owner, "delivery-parent")
	parent := parentReq.Event.Source
	if _, err := repo.Register(ctx, owner, parentReq); err != nil {
		t.Fatal(err)
	}
	childReq := journalRegistration(owner, "delivery-child")
	child := childReq.Event.Source
	child.NodeID = "node-derived"
	child.SourceEntryID = "source-derived"
	child.SourceVersion = "1"
	child.Kind = "DERIVED"
	child.SourceRef.ObjectID = "source-derived"
	child.ParentNodeIDs = []string{parent.NodeID}
	childReq.Event.Source = child
	childReq.Event.ParentNodeIDs = []string{parent.NodeID}
	childReq.Event.Kind = "HISTORY_SUMMARY"
	childReq.Closure = []inputartifact.SourceNode{parent, child}
	if _, err := repo.Register(ctx, owner, childReq); err != nil {
		t.Fatalf("valid derived registration: %v", err)
	}
	view, err := repo.ReadJournal(ctx, owner, childReq.Event.JournalKey, reqScope(childReq))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Nodes) != 2 || !slices.Equal(view.ReverseDependents[parent.NodeID], []string{child.NodeID}) {
		t.Fatalf("nodes=%+v reverse=%+v", view.Nodes, view.ReverseDependents)
	}

	orphanReq := journalRegistration(owner, "delivery-orphan")
	orphan := orphanReq.Event.Source
	orphan.NodeID = "node-orphan"
	orphan.SourceEntryID = "source-orphan"
	orphan.Kind = "DERIVED"
	orphan.SourceRef.ObjectID = "source-orphan"
	orphan.ParentNodeIDs = []string{"node-missing"}
	orphanReq.Event.Source = orphan
	orphanReq.Event.ParentNodeIDs = []string{"node-missing"}
	orphanReq.Closure = []inputartifact.SourceNode{orphan}
	if _, err := repo.Register(ctx, owner, orphanReq); !errors.Is(err, readsetjournal.ErrDependency) {
		t.Fatalf("orphan derived registration error=%v", err)
	}
	view, err = repo.ReadJournal(ctx, owner, childReq.Event.JournalKey, reqScope(childReq))
	if err != nil || view.HighWatermark != 2 {
		t.Fatalf("orphan changed journal: view=%+v err=%v", view, err)
	}
}

func TestReadSetJournal_DefaultDisabledDoesNotWriteMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-disabled")
	repo := newReadSetJournalRepo(db, false)
	if _, err := repo.Register(context.Background(), owner, journalRegistration(owner, "delivery-disabled")); !errors.Is(err, readsetjournal.ErrDisabled) {
		t.Fatalf("disabled error=%v", err)
	}
	var count int64
	if err := db.Table("readset_journal").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("disabled path count=%d err=%v", count, err)
	}
}

func TestReadSetJournal_MigrationShapeMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	migrateReadSetJournalMySQL(t, db)
	required := map[string][]string{
		"readset_journal": {
			"high_watermark", "execution_generation", "owner_lease_until", "admission_scope_id",
			"context_revocation_epoch", "authority_epoch", "credential_epoch", "state", "state_version",
		},
		"readset_journal_event": {"event_version", "delivery_id", "payload_json", "payload_digest", "request_digest"},
		"readset_journal_delivery": {
			"state", "last_execution_generation", "last_owner_job_id", "last_owner_worker_id",
			"last_owner_lease_until", "authority_sequence", "verification_record_json", "injected_at", "verified_at",
		},
		"readset_journal_node": {"node_json", "node_digest", "admission_authority_sequence"},
		"readset_journal_edge": {"parent_node_id", "child_node_id"},
	}
	for table, columns := range required {
		var present []string
		if err := db.Raw(`SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&present).Error; err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			if !slices.Contains(present, column) {
				t.Errorf("%s missing column %s; columns=%v", table, column, present)
			}
		}
	}
	var reverseIndexes []string
	if err := db.Raw(`SELECT DISTINCT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'readset_journal_edge' AND INDEX_NAME = 'idx_readset_edge_reverse'`).
		Scan(&reverseIndexes).Error; err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(reverseIndexes, "idx_readset_edge_reverse") {
		t.Fatalf("reverse dependency index missing: %v", reverseIndexes)
	}
}

func TestReadSetJournal_ReplayNeverPermitsExposureMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-replay-permit")
	repo := newReadSetJournalRepo(db, true)
	req := journalRegistration(owner, "delivery-replay-permit")

	first, err := repo.Register(ctx, owner, req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || !first.ExposurePermitted || first.RequiresReconciliation {
		t.Fatalf("new registration must be the sole exposure permit: %+v", first)
	}
	for _, state := range []readsetjournal.ProviderState{
		readsetjournal.ProviderPending,
		readsetjournal.ProviderUnknown,
		readsetjournal.ProviderUnverifiable,
	} {
		switch state {
		case readsetjournal.ProviderUnknown:
			if _, err := repo.MarkProviderUnknown(ctx, owner, req.Event.JournalKey, reqScope(req), req.Event.DeliveryID, "timeout"); err != nil {
				t.Fatal(err)
			}
		case readsetjournal.ProviderUnverifiable:
			if _, err := repo.MarkProviderUnverifiable(ctx, owner, req.Event.JournalKey, reqScope(req), req.Event.DeliveryID, "provider unavailable"); err != nil {
				t.Fatal(err)
			}
		}
		replay, err := repo.Register(ctx, owner, req)
		if err != nil {
			t.Fatalf("state %s replay: %v", state, err)
		}
		if replay.Created || replay.ExposurePermitted || !replay.RequiresReconciliation {
			t.Fatalf("state %s duplicate must require reconciliation and never permit exposure: %+v", state, replay)
		}
	}
}

func TestReadSetJournal_RegisterRejectsStaleSourceMySQL(t *testing.T) {
	expiredUntil := "2021-01-01T00:00:00.000Z"
	cases := []struct {
		name       string
		notBefore  string
		validUntil *string
	}{
		{name: "expired", notBefore: "2020-01-01T00:00:00.000Z", validUntil: &expiredUntil},
		{name: "not_yet_valid", notBefore: "2099-01-01T00:00:00.000Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			_, jobs, job := seedArtifactGenerationJob(t, db)
			migrateReadSetJournalMySQL(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "journal-freshness-"+tc.name)
			repo := newReadSetJournalRepo(db, true)
			req := journalRegistration(owner, "delivery-freshness-"+tc.name)
			req.Event.Source.Validity = inputartifact.Validity{NotBefore: tc.notBefore, ValidUntil: tc.validUntil}
			req.Closure[0] = req.Event.Source
			result, err := repo.Register(ctx, owner, req)
			if !errors.Is(err, readsetjournal.ErrStaleMaterial) {
				t.Fatalf("stale registration result=%+v error=%v, want ErrStaleMaterial", result, err)
			}
			if result.Created || result.ExposurePermitted || result.RequiresReconciliation {
				t.Fatalf("stale registration returned partial result: %+v", result)
			}
			for _, table := range []string{"readset_journal", "readset_journal_event", "readset_journal_delivery", "readset_journal_node"} {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("stale registration wrote %s: count=%d err=%v", table, count, err)
				}
			}
		})
	}
}

func TestReadSetJournal_ReadRechecksFreshnessMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-read-freshness")
	repo := newReadSetJournalRepo(db, true)
	req := journalRegistration(owner, "delivery-read-freshness")
	if _, err := repo.Register(ctx, owner, req); err != nil {
		t.Fatal(err)
	}

	expiredUntil := "2021-01-01T00:00:00.000Z"
	staleReq := req
	staleReq.Event.Source.Validity = inputartifact.Validity{
		NotBefore: "2020-01-01T00:00:00.000Z", ValidUntil: &expiredUntil,
	}
	staleReq.Closure[0] = staleReq.Event.Source
	cfg := readsetjournal.DefaultConfig()
	cfg.Enabled = true
	prepared, err := readsetjournal.PrepareRegistration(cfg, staleReq)
	if err != nil {
		t.Fatal(err)
	}
	journalID := readSetJournalIDForCase(t, db, owner.CaseID)
	if err := db.Exec(`UPDATE readset_journal_node SET node_json = ?, node_digest = ?
		WHERE journal_id = ? AND node_id = ?`,
		prepared.Nodes[0].Bytes, prepared.Nodes[0].Digest, journalID, staleReq.Event.Source.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE readset_journal_event
		SET payload_json = ?, payload_digest = ?, request_digest = ?, payload_bytes = ?
		WHERE journal_id = ? AND delivery_id = ?`,
		prepared.EventBytes, prepared.EventDigest, prepared.RequestDigest, len(prepared.EventBytes),
		journalID, staleReq.Event.DeliveryID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE readset_journal_delivery SET payload_digest = ?
		WHERE journal_id = ? AND delivery_id = ?`,
		prepared.EventDigest, journalID, staleReq.Event.DeliveryID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE readset_journal
		SET event_bytes = (SELECT COALESCE(SUM(payload_bytes), 0) FROM readset_journal_event WHERE journal_id = ?)
		WHERE id = ?`, journalID, journalID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadJournal(ctx, owner, staleReq.Event.JournalKey, reqScope(staleReq)); !errors.Is(err, readsetjournal.ErrStaleMaterial) {
		t.Fatalf("read-time freshness error=%v, want ErrStaleMaterial", err)
	}
}

func TestReadSetJournal_GenerationCannotAppendSecondEventMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	first, owner1 := claimArtifactOwner(t, jobs, job, "journal-generation-anchor")
	repo := newReadSetJournalRepo(db, true)
	original := journalRegistration(owner1, "delivery-generation-1")
	registered, err := repo.Register(ctx, owner1, original)
	if err != nil {
		t.Fatal(err)
	}

	requeueArtifactOwner(t, jobs, first, owner1.WorkerID)
	_, owner2 := claimArtifactOwner(t, jobs, job, owner1.WorkerID)
	if owner2.ExecutionGeneration == owner1.ExecutionGeneration {
		t.Fatal("generation did not advance")
	}
	if _, err := repo.Register(ctx, owner2, journalRegistration(owner2, "delivery-generation-2")); !errors.Is(err, readsetjournal.ErrOwnerFence) {
		t.Fatalf("cross-generation append error=%v, want ErrOwnerFence", err)
	}
	view, err := repo.ReadJournal(ctx, owner2, original.Event.JournalKey, reqScope(original))
	if err != nil {
		t.Fatal(err)
	}
	if view.HighWatermark != 1 || len(view.Events) != 1 || view.Events[0].Sequence != registered.Sequence {
		t.Fatalf("cross-generation append increased HWM: %+v", view)
	}

	replayReq := original
	replayReq.Owner = readsetjournal.OwnerFence{
		CaseID: owner2.CaseID, JobID: owner2.JobID, WorkerID: owner2.WorkerID,
		ExecutionGeneration: owner2.ExecutionGeneration,
	}
	replay, err := repo.Register(ctx, owner2, replayReq)
	if err != nil {
		t.Fatalf("new generation exact replay: %v", err)
	}
	if replay.Created || replay.ExposurePermitted || !replay.RequiresReconciliation || replay.Sequence != registered.Sequence {
		t.Fatalf("new generation replay semantics=%+v", replay)
	}
}

func TestReadSetJournal_ReadbackRecomputesClosureEvidenceMySQL(t *testing.T) {
	registerDerived := func(t *testing.T, db *gorm.DB, owner *entity.ExecutionContext, suffix string) (readsetjournal.RegisterRequest, readsetjournal.JournalKey, string) {
		t.Helper()
		req := journalRegistration(owner, "delivery-evidence-"+suffix)
		parent := req.Event.Source
		child := parent
		child.NodeID = "node-evidence-child"
		child.SourceEntryID = "source-evidence-child"
		child.Kind = "DERIVED"
		child.SourceRef.ObjectID = "source-evidence-child"
		child.ParentNodeIDs = []string{parent.NodeID}
		req.Event.Source = child
		req.Event.ParentNodeIDs = []string{parent.NodeID}
		req.Event.Kind = "HISTORY_SUMMARY"
		req.Closure = []inputartifact.SourceNode{parent, child}
		if _, err := newReadSetJournalRepo(db, true).Register(context.Background(), owner, req); err != nil {
			t.Fatalf("register evidence fixture: %v", err)
		}
		return req, req.Event.JournalKey, readSetJournalIDForCase(t, db, owner.CaseID)
	}

	cases := []struct {
		name    string
		corrupt func(t *testing.T, db *gorm.DB, journalID, parentID, childID string)
	}{
		{
			name: "request_digest",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, _, _ string) {
				if err := db.Exec("UPDATE readset_journal_event SET request_digest = ? WHERE journal_id = ?", strings.Repeat("0", 64), journalID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "node_first_sequence",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, _, childID string) {
				if err := db.Exec("UPDATE readset_journal_node SET first_sequence = 2 WHERE journal_id = ? AND node_id = ?", journalID, childID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "node_admission_generation",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, _, childID string) {
				if err := db.Exec("UPDATE readset_journal_node SET admission_generation = admission_generation + 1 WHERE journal_id = ? AND node_id = ?", journalID, childID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "node_admission_scope",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, _, childID string) {
				if err := db.Exec("UPDATE readset_journal_node SET admission_scope_id = 'foreign-scope' WHERE journal_id = ? AND node_id = ?", journalID, childID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "edge_created_sequence",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, parentID, childID string) {
				if err := db.Exec("UPDATE readset_journal_edge SET created_sequence = 99 WHERE journal_id = ? AND parent_node_id = ? AND child_node_id = ?", journalID, parentID, childID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "parent_admission_order",
			corrupt: func(t *testing.T, db *gorm.DB, journalID, parentID, _ string) {
				if err := db.Exec("UPDATE readset_journal_node SET first_sequence = 2 WHERE journal_id = ? AND node_id = ?", journalID, parentID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			_, jobs, job := seedArtifactGenerationJob(t, db)
			migrateReadSetJournalMySQL(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "journal-evidence-"+tc.name)
			req, key, journalID := registerDerived(t, db, owner, tc.name)
			tc.corrupt(t, db, journalID, req.Closure[0].NodeID, req.Event.Source.NodeID)
			if view, err := newReadSetJournalRepo(db, true).ReadJournal(ctx, owner, key, reqScope(req)); err == nil {
				t.Fatalf("corrupt closure/evidence accepted for restore: %+v", view)
			}
		})
	}
}

func TestReadSetJournal_FrozenDeclarationRejectionsMySQL(t *testing.T) {
	appendCases := []struct {
		name   string
		mutate func(*readsetjournal.RegisterRequest)
	}{
		{
			name: "future_readset_source",
			mutate: func(req *readsetjournal.RegisterRequest) {
				req.Event.Source.SourceEntryID = "rset_" + journalDigest("future-rset")
				req.Event.Source.SourceRef.ObjectID = req.Event.Source.SourceEntryID
				req.Closure[0] = req.Event.Source
			},
		},
		{
			name: "debate_initial",
			mutate: func(req *readsetjournal.RegisterRequest) {
				req.Event.Kind = "DEBATE"
				req.Event.Phase = "INITIAL_REVIEW"
			},
		},
	}
	for _, tc := range appendCases {
		t.Run("append_"+tc.name, func(t *testing.T) {
			db := openArtifactGenerationMySQL(t)
			_, jobs, job := seedArtifactGenerationJob(t, db)
			migrateReadSetJournalMySQL(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "journal-declaration-"+tc.name)
			req := journalRegistration(owner, "delivery-declaration-"+tc.name)
			tc.mutate(&req)
			if _, err := newReadSetJournalRepo(db, true).Register(context.Background(), owner, req); err == nil {
				t.Fatal("frozen ReadSet declaration violation was admitted")
			}
		})
	}

	t.Run("phase_regression_append", func(t *testing.T) {
		ctx := context.Background()
		db := openArtifactGenerationMySQL(t)
		_, jobs, job := seedArtifactGenerationJob(t, db)
		migrateReadSetJournalMySQL(t, db)
		_, owner := claimArtifactOwner(t, jobs, job, "journal-phase-regression")
		repo := newReadSetJournalRepo(db, true)
		first := journalRegistration(owner, "delivery-phase-cross")
		first.Event.Phase = "CROSS_EXAMINATION"
		if _, err := repo.Register(ctx, owner, first); err != nil {
			t.Fatal(err)
		}
		regressed := journalRegistration(owner, "delivery-phase-initial")
		regressed.Event.Phase = "INITIAL_REVIEW"
		if _, err := repo.Register(ctx, owner, regressed); !errors.Is(err, readsetjournal.ErrInvalidJournal) {
			t.Fatalf("phase regression append error=%v", err)
		}
		view, err := repo.ReadJournal(ctx, owner, first.Event.JournalKey, reqScope(first))
		if err != nil || view.HighWatermark != 1 {
			t.Fatalf("phase regression changed journal: view=%+v err=%v", view, err)
		}
	})

	t.Run("phase_regression_readback", func(t *testing.T) {
		ctx := context.Background()
		db := openArtifactGenerationMySQL(t)
		_, jobs, job := seedArtifactGenerationJob(t, db)
		migrateReadSetJournalMySQL(t, db)
		_, owner := claimArtifactOwner(t, jobs, job, "journal-phase-readback")
		repo := newReadSetJournalRepo(db, true)
		first := journalRegistration(owner, "delivery-phase-readback-1")
		first.Event.Phase = "CROSS_EXAMINATION"
		if _, err := repo.Register(ctx, owner, first); err != nil {
			t.Fatal(err)
		}
		second := journalRegistration(owner, "delivery-phase-readback-2")
		second.Event.Phase = "FINAL_REVIEW"
		if _, err := repo.Register(ctx, owner, second); err != nil {
			t.Fatal(err)
		}
		corrupted := second
		corrupted.Event.Phase = "INITIAL_REVIEW"
		cfg := readsetjournal.DefaultConfig()
		cfg.Enabled = true
		prepared, err := readsetjournal.PrepareRegistration(cfg, corrupted)
		if err != nil {
			t.Fatal(err)
		}
		journalID := readSetJournalIDForCase(t, db, owner.CaseID)
		if err := db.Exec(`UPDATE readset_journal_event
			SET payload_json = ?, payload_digest = ?, request_digest = ?, payload_bytes = ?
			WHERE journal_id = ? AND delivery_id = ?`,
			prepared.EventBytes, prepared.EventDigest, prepared.RequestDigest, len(prepared.EventBytes),
			journalID, second.Event.DeliveryID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`UPDATE readset_journal_delivery SET payload_digest = ?
			WHERE journal_id = ? AND delivery_id = ?`,
			prepared.EventDigest, journalID, second.Event.DeliveryID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`UPDATE readset_journal
			SET event_bytes = (SELECT COALESCE(SUM(payload_bytes), 0) FROM readset_journal_event WHERE journal_id = ?)
			WHERE id = ?`, journalID, journalID).Error; err != nil {
			t.Fatal(err)
		}
		if view, err := repo.ReadJournal(ctx, owner, second.Event.JournalKey, reqScope(second)); err == nil {
			t.Fatalf("phase-regressed journal accepted for restore: %+v", view)
		}
	})
}

func TestReadSetJournal_ReadbackValidatesGenerationAndOwnerMySQL(t *testing.T) {
	type corruption struct {
		name        string
		expectDBErr bool
		apply       func(t *testing.T, db *gorm.DB, journalID string)
	}
	cases := []corruption{
		{name: "journal_owner_job", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal SET owner_job_id = 'foreign-journal-job' WHERE id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "journal_owner_worker", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal SET owner_worker_id = 'foreign-journal-worker' WHERE id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_generation_anchor", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET execution_generation = execution_generation + 1 WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "delivery_first_generation_anchor", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET first_execution_generation = first_execution_generation + 1 WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_generation_zero", expectDBErr: true, apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET execution_generation = 0 WHERE journal_id = ?", journalID).Error; err == nil {
				t.Fatal("database accepted event generation 0")
			}
		}},
		{name: "delivery_first_generation_zero", expectDBErr: true, apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET first_execution_generation = 0 WHERE journal_id = ?", journalID).Error; err == nil {
				t.Fatal("database accepted delivery generation 0")
			}
		}},
		{name: "event_owner_job", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET owner_job_id = '' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_owner_worker", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET owner_worker_id = '' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_owner_job_foreign", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET owner_job_id = 'foreign-event-job' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_owner_worker_foreign", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET owner_worker_id = 'foreign-event-worker' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "node_admission_job", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_node SET admission_job_id = 'foreign-node-job' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "node_admission_worker", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_node SET admission_worker_id = 'foreign-node-worker' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_lease_evidence", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_event SET owner_lease_until = '1970-01-01 00:00:00' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "delivery_owner_job", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET last_owner_job_id = '' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "delivery_owner_worker", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET last_owner_worker_id = '' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "delivery_lease_evidence", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET last_owner_lease_until = '1970-01-01 00:00:00' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event_delivery_provider_request_identity", apply: func(t *testing.T, db *gorm.DB, journalID string) {
			if err := db.Exec("UPDATE readset_journal_delivery SET provider_request_id = 'foreign-provider-request' WHERE journal_id = ?", journalID).Error; err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			_, jobs, job := seedArtifactGenerationJob(t, db)
			migrateReadSetJournalMySQL(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "journal-evidence-owner-"+tc.name)
			req := journalRegistration(owner, "delivery-evidence-owner-"+tc.name)
			repo := newReadSetJournalRepo(db, true)
			if _, err := repo.Register(ctx, owner, req); err != nil {
				t.Fatal(err)
			}
			journalID := readSetJournalIDForCase(t, db, owner.CaseID)
			tc.apply(t, db, journalID)
			if tc.expectDBErr {
				return
			}
			if view, err := repo.ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req)); err == nil {
				t.Fatalf("corrupt generation/owner evidence accepted: %+v", view)
			}
		})
	}

	t.Run("delivery_generation_two_transition_allowed", func(t *testing.T) {
		ctx := context.Background()
		db := openArtifactGenerationMySQL(t)
		_, jobs, job := seedArtifactGenerationJob(t, db)
		migrateReadSetJournalMySQL(t, db)
		_, owner := claimArtifactOwner(t, jobs, job, "journal-delivery-gen2-trace")
		req := journalRegistration(owner, "delivery-gen2-trace")
		repo := newReadSetJournalRepo(db, true)
		if _, err := repo.Register(ctx, owner, req); err != nil {
			t.Fatal(err)
		}
		journalID := readSetJournalIDForCase(t, db, owner.CaseID)
		if err := db.Exec(`UPDATE readset_journal_delivery SET
			last_execution_generation = first_execution_generation + 1,
			last_owner_job_id = 'generation-2-job',
			last_owner_worker_id = 'generation-2-worker',
			last_owner_lease_until = DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL 1 MINUTE)
			WHERE journal_id = ?`, journalID).Error; err != nil {
			t.Fatal(err)
		}
		view, err := repo.ReadJournal(ctx, owner, req.Event.JournalKey, reqScope(req))
		if err != nil {
			t.Fatalf("generation-2 delivery trace rejected: %v", err)
		}
		if view.HighWatermark != 1 {
			t.Fatalf("delivery trace changed journal anchor: %+v", view)
		}
	})
}

func TestReadSetJournal_ReadRevalidatesLeaseAfterLongValidationMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	migrateReadSetJournalMySQL(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "journal-read-lease")
	repo := newReadSetJournalRepo(db, true)
	req := journalRegistration(owner, "delivery-read-lease")
	if _, err := repo.Register(ctx, owner, req); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE decision_job SET lease_until = DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id = ?", owner.JobID).Error; err != nil {
		t.Fatal(err)
	}
	name := "readset-journal:slow-read"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "readset_journal_node" && tx.Statement.Context.Value(t4ContextKey{}) == "slow-read" {
			time.Sleep(1500 * time.Millisecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	waitCtx, cancel := context.WithTimeout(context.WithValue(ctx, t4ContextKey{}, "slow-read"), 5*time.Second)
	defer cancel()
	if view, err := repo.ReadJournal(waitCtx, owner, req.Event.JournalKey, reqScope(req)); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("slow read view=%+v error=%v, want ErrLeaseLost", view, err)
	}
}
