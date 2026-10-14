package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// s29ResolutionColumns are the resolution columns published before S30. The
// upgrade path must add dissent_json without touching them.
const s29ResolutionColumns = `(id, case_id, execution_generation, consensus_json, final_decision,
	final_report, key_evidence_ids_json, key_claim_ids_json, vote_ids_json, evaluation_json)`

// rollResolutionBackToS29 drops dissent_json, reproducing a database that was
// migrated up to S29 only.
func rollResolutionBackToS29(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ALTER TABLE resolution DROP COLUMN dissent_json").Error; err != nil {
		t.Fatalf("roll back resolution to S29 shape: %v", err)
	}
}

// assertS30DissentColumn checks the shape AND the tri-state contract of the
// column: TEXT, nullable, no default, so a legacy row reads as "not recorded"
// instead of an implicit "no dissent".
func assertS30DissentColumn(t *testing.T, db *gorm.DB) writerColumn {
	t.Helper()
	var row writerColumn
	if err := db.Raw(`SELECT COLUMN_NAME AS column_name, DATA_TYPE AS data_type, COLUMN_TYPE AS column_type,
			IS_NULLABLE AS is_nullable, COLUMN_DEFAULT AS column_default,
			CHARACTER_MAXIMUM_LENGTH AS character_maximum_length
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'resolution' AND COLUMN_NAME = 'dissent_json'`).
		Scan(&row).Error; err != nil {
		t.Fatalf("read resolution.dissent_json: %v", err)
	}
	if row.Name == "" {
		t.Fatal("resolution.dissent_json is missing")
	}
	if row.Type != "text" || row.Nullable != "YES" || row.Default != nil {
		t.Fatalf("resolution.dissent_json shape = %+v, want nullable TEXT with no default", row)
	}
	return row
}

// TestMySQLMigration_S30DissentTriState proves the S29 -> S30 upgrade preserves
// historical resolutions and that NULL, "[]" and "[...]" stay distinguishable
// through a real MySQL round trip.
func TestMySQLMigration_S30DissentTriState(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := provideDBForTest(t, dsn)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	rollResolutionBackToS29(t, db)
	// A resolution written before S30 carries no minority record at all.
	if err := db.Exec(`INSERT INTO resolution `+s29ResolutionColumns+`
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"res-legacy", "legacy-case", 0, "{}", "approve", "# legacy report", "[]", "[]", "[]", "null").Error; err != nil {
		t.Fatalf("seed legacy resolution: %v", err)
	}

	applyMySQLScript(t, db, s30MigrationPath)
	assertS30DissentColumn(t, db)

	legacy, err := repo.ResolutionRepo().Get(ctx, "legacy-case")
	if err != nil || legacy == nil {
		t.Fatalf("legacy resolution lost: %+v err=%v", legacy, err)
	}
	if legacy.FinalReport != "# legacy report" {
		t.Fatalf("legacy report changed by migration: %q", legacy.FinalReport)
	}
	if legacy.Dissent != nil || legacy.DissentState() != entity.DissentUnknown {
		t.Fatalf("legacy resolution must read as UNKNOWN, got dissent=%+v state=%s", legacy.Dissent, legacy.DissentState())
	}

	// An explicit "recorded no dissent" is a non-nil empty slice.
	none := &entity.Resolution{ID: "res-none", CaseID: "case-none", FinalDecision: entity.VoteDecisionApprove,
		FinalReport: "# none", Dissent: []entity.Dissent{}}
	if err := repo.ResolutionRepo().Create(ctx, none); err != nil {
		t.Fatalf("write recorded-none: %v", err)
	}
	gotNone, err := repo.ResolutionRepo().Get(ctx, "case-none")
	if err != nil || gotNone == nil {
		t.Fatalf("read recorded-none: %+v err=%v", gotNone, err)
	}
	if gotNone.Dissent == nil || gotNone.DissentState() != entity.DissentNone {
		t.Fatalf("recorded-none collapsed: dissent=%+v state=%s", gotNone.Dissent, gotNone.DissentState())
	}

	// A recorded minority survives with its conditions.
	present := &entity.Resolution{ID: "res-present", CaseID: "case-present", FinalDecision: entity.VoteDecisionApprove,
		FinalReport: "# present",
		Dissent: []entity.Dissent{{
			AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject,
			Reasoning: "unproven worst case", EvidenceIDs: []string{"EV-1"},
			Conditions: []entity.DecisionCondition{{Statement: "require rollback plan", MustHold: true}},
		}}}
	if err := repo.ResolutionRepo().Create(ctx, present); err != nil {
		t.Fatalf("write recorded-minority: %v", err)
	}
	gotPresent, err := repo.ResolutionRepo().Get(ctx, "case-present")
	if err != nil || gotPresent == nil || gotPresent.DissentState() != entity.DissentPresent {
		t.Fatalf("recorded-minority round trip: %+v err=%v", gotPresent, err)
	}
	if len(gotPresent.Dissent) != 1 || gotPresent.Dissent[0].AgentCode != entity.MagiCodeCasper ||
		gotPresent.Dissent[0].Reasoning != "unproven worst case" || len(gotPresent.Dissent[0].Conditions) != 1 {
		t.Fatalf("recorded-minority content lost: %+v", gotPresent.Dissent)
	}

	// A restart must not reinterpret any of the three states.
	provideDBForTest(t, dsn)
	for caseID, want := range map[string]entity.DissentState{
		"legacy-case": entity.DissentUnknown, "case-none": entity.DissentNone, "case-present": entity.DissentPresent,
	} {
		res, err := repo.ResolutionRepo().Get(ctx, caseID)
		if err != nil || res == nil || res.DissentState() != want {
			t.Fatalf("after restart %s: state=%v want=%v err=%v", caseID, res.DissentState(), want, err)
		}
	}
}

// TestMySQLMigration_S30FreshInitMatchesMigration proves a brand-new database
// (AutoMigrate) and an upgraded one (S30 script) agree on the column contract.
func TestMySQLMigration_S30FreshInitMatchesMigration(t *testing.T) {
	fresh := provideDBForTest(t, newMySQLSchema(t))
	migrated := provideDBForTest(t, newMySQLSchema(t))
	rollResolutionBackToS29(t, migrated)
	applyMySQLScript(t, migrated, s30MigrationPath)

	freshCol := assertS30DissentColumn(t, fresh)
	migratedCol := assertS30DissentColumn(t, migrated)
	if freshCol.FullType != migratedCol.FullType || freshCol.Nullable != migratedCol.Nullable {
		t.Fatalf("fresh %+v != migrated %+v", freshCol, migratedCol)
	}
}

// TestMySQLDecisionWriter_TerminalFailureLeavesNoResolution proves the terminal
// transaction never leaves a partial authoritative Resolution (and therefore no
// half-committed FinalReport or Dissent) when the commit fails.
func TestMySQLDecisionWriter_TerminalFailureLeavesNoResolution(t *testing.T) {
	_, db := t6SchemaFixture(t)
	repo := magi.NewDecisionWriterRepository(db)
	ctx := context.Background()

	c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	if err := repo.CaseRepo().Create(ctx, c); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	job, admitted, err := jobs.Admit(ctx, c.ID, 3, 0)
	if err != nil || !admitted {
		t.Fatalf("admit: job=%+v admitted=%v err=%v", job, admitted, err)
	}
	claimed, ok, err := jobs.Claim(ctx, job.ID, "worker-s30", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	owner := &entity.ExecutionContext{CaseID: c.ID, JobID: job.ID, WorkerID: "worker-s30", ExecutionGeneration: claimed.ExecutionGeneration}
	owned := repo.(port.OwnedCaseCommitter)

	statusEvent := entity.NewEvent(c.ID, "", nil, entity.EventCaseStatusChanged, nil)
	statusEvent.ExecutionGeneration = owner.ExecutionGeneration
	if ok, err := owned.CommitStatusTransitionOwned(ctx, owner, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusResolving, &statusEvent); err != nil || !ok {
		t.Fatalf("advance to resolving: ok=%v err=%v", ok, err)
	}

	// The resolutions cites an evidence row that does not exist in this
	// generation, so the in-transaction reference check rejects the commit
	// after the case row is locked.
	res := &entity.Resolution{
		ID: uuid.NewString(), CaseID: c.ID, ExecutionGeneration: owner.ExecutionGeneration,
		FinalDecision: entity.VoteDecisionApprove, FinalReport: "# half-committed report",
		KeyEvidenceIDs: []string{"does-not-exist-EV-001"},
		Dissent: []entity.Dissent{{
			AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject, Reasoning: "risk",
		}},
	}
	doneEvent := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
	doneEvent.ExecutionGeneration = owner.ExecutionGeneration
	committed, err := owned.CommitTerminalOwned(ctx, owner, entity.CaseStatusResolving, entity.CaseStatusResolved, res, &doneEvent)
	if err == nil || committed {
		t.Fatalf("terminal commit with an unknown citation must fail: committed=%v err=%v", committed, err)
	}

	if got, _ := repo.ResolutionRepo().Get(ctx, c.ID); got != nil {
		t.Fatalf("partial resolution persisted: %+v", got)
	}
	var count int64
	if err := db.Table("resolution").Where("case_id = ?", c.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("resolution rows for failed commit = %d, want 0", count)
	}
	fresh, _ := repo.CaseRepo().Get(ctx, c.ID)
	if fresh == nil || fresh.Status != entity.CaseStatusResolving {
		t.Fatalf("case status must stay RESOLVING after rollback, got %+v", fresh)
	}
}
