package bootstrap

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

const s28MigrationPath = "../../docker/atlas/migrations/magi_s28_event_generation.sql"

func TestMySQLMigration_S28EventGeneration(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := provideDBForTest(t, dsn)
	repo := magi.NewRepository(db)
	event := entity.NewEvent("legacy-case", "legacy-run", nil, entity.EventCaseCompleted, nil)
	if err := repo.EventRepo().Create(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE magi_event DROP COLUMN execution_generation").Error; err != nil {
		t.Fatal(err)
	}
	applyMySQLScript(t, db, s28MigrationPath)
	stored, err := repo.EventRepo().ListByCase(context.Background(), event.CaseID)
	if err != nil || len(stored) != 1 || stored[0].ExecutionGeneration != 0 || stored[0].ID != event.ID || stored[0].Seq != 1 {
		t.Fatalf("legacy event=%+v err=%v", stored, err)
	}
	next := entity.NewEvent(event.CaseID, "new-run", nil, entity.EventAgentStarted, nil)
	next.ExecutionGeneration = 7
	if err := repo.EventRepo().Create(context.Background(), &next); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.EventRepo().ListAfterSeq(context.Background(), event.CaseID, 1, 10)
	if err != nil || len(stored) != 1 || stored[0].ExecutionGeneration != 7 || stored[0].Seq != 2 {
		t.Fatalf("new event=%+v err=%v", stored, err)
	}
	// Existing installations are also prepared safely by bootstrap, after S16
	// sequence verification; a repeated start does not alter provenance.
	provideDBForTest(t, dsn)
	stored, err = repo.EventRepo().ListByCase(context.Background(), event.CaseID)
	if err != nil || len(stored) != 2 || stored[0].ExecutionGeneration != 0 || stored[1].ExecutionGeneration != 7 {
		t.Fatalf("restart events=%+v err=%v", stored, err)
	}
}

func TestMySQLMigration_S28BootstrapUpgrade(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := provideDBForTest(t, dsn)
	repo := magi.NewRepository(db)
	event := entity.NewEvent("old-case", "", nil, entity.EventCaseCompleted, nil)
	if err := repo.EventRepo().Create(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE magi_event DROP COLUMN execution_generation").Error; err != nil {
		t.Fatal(err)
	}
	provideDBForTest(t, dsn)
	stored, err := repo.EventRepo().ListByCase(context.Background(), event.CaseID)
	if err != nil || len(stored) != 1 || stored[0].ExecutionGeneration != 0 || stored[0].Seq != 1 || stored[0].ID != event.ID {
		t.Fatalf("bootstrap upgrade lost legacy provenance: %+v err=%v", stored, err)
	}
}
