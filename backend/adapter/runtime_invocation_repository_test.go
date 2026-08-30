package magi_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openRuntimeInvocationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func openRuntimeInvocationConcurrentDB(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "runtime_invocation.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("sql db: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	dbA := open()
	dbB := open()
	if err := dbA.AutoMigrate(&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return dbA, dbB
}

func seedRuntimeInvocation(t *testing.T, db *gorm.DB, id, idempotencyKey string) {
	t.Helper()
	if err := db.Create(&magi.RuntimeInvocationModel{
		InvocationID: id,
		RunID:        "run-1",
		StepID:       "step-1",
		Kind:         "tool",
		Status:       string(entity.InvocationPending),
		InputJSON:    `{"input":"value"}`,
		IdempotencyKey: func() *string {
			if idempotencyKey == "" {
				return nil
			}
			return &idempotencyKey
		}(),
	}).Error; err != nil {
		t.Fatalf("seed invocation: %v", err)
	}
}

func TestRuntimeInvocationRepository(t *testing.T) {
	t.Run("BeginIsSingleWinner", TestInvocationRepository_BeginIsSingleWinner)
	t.Run("CompletedResultIsReusable", TestInvocationRepository_CompletedResultIsReusable)
	t.Run("LateAttemptCannotOverwriteNewAttempt", TestInvocationRepository_LateAttemptCannotOverwriteNewAttempt)
	t.Run("IdempotencyKeyUnique", TestInvocationRepository_IdempotencyKeyUnique)
}

func TestInvocationRepository_BeginIsSingleWinner(t *testing.T) {
	dbA, dbB := openRuntimeInvocationConcurrentDB(t)
	seedRuntimeInvocation(t, dbA, "invocation-1", "idem-1")

	type beginResult struct {
		invocation *entity.RuntimeInvocation
		won        bool
		err        error
	}
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan beginResult, 2)
	var wg sync.WaitGroup
	for attemptID, repo := range map[string]port.RuntimeInvocationRepository{
		"attempt-1": magi.NewRuntimeInvocationRepository(dbA),
		"attempt-2": magi.NewRuntimeInvocationRepository(dbB),
	} {
		wg.Add(1)
		go func(attemptID string, repo port.RuntimeInvocationRepository) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			invocation, won, err := repo.BeginAttempt(context.Background(), "invocation-1", attemptID)
			results <- beginResult{invocation: invocation, won: won, err: err}
		}(attemptID, repo)
	}
	for range 2 {
		<-ready
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent begin: invocation=%+v won=%v err=%v", result.invocation, result.won, result.err)
		}
		if result.won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent begin winners = %d, want 1", winners)
	}

	var attempts int64
	if err := dbA.Model(&magi.RuntimeInvocationAttemptModel{}).Where("invocation_id = ?", "invocation-1").Count(&attempts).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempt rows = %d, want 1", attempts)
	}
}

func TestRuntimeInvocationModel_AutoMigrateSchemaMatchesS21(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	type columnInfo struct {
		Name       string
		Type       string
		NotNull    int     `gorm:"column:notnull"`
		DefaultVal *string `gorm:"column:dflt_value"`
	}
	columns := make(map[string]columnInfo)
	var rows []columnInfo
	if err := db.Raw("PRAGMA table_info(runtime_invocation)").Scan(&rows).Error; err != nil {
		t.Fatalf("table info: %v", err)
	}
	for _, column := range rows {
		columns[column.Name] = column
	}
	for name, want := range map[string]struct {
		typeName string
		notNull  bool
		default_ *string
	}{
		"input_json":     {typeName: "mediumtext", notNull: true},
		"output_json":    {typeName: "mediumtext", notNull: false},
		"attempt_count":  {typeName: "integer", notNull: true, default_: stringPtr("0")},
		"operation_name": {notNull: true, default_: stringPtr("''")},
		"input_digest":   {notNull: true, default_: stringPtr("''")},
	} {
		got, ok := columns[name]
		if !ok {
			t.Fatalf("missing column %q", name)
		}
		if (want.typeName != "" && !strings.EqualFold(got.Type, want.typeName)) || (got.NotNull != 0) != want.notNull || !sameDefault(got.DefaultVal, want.default_) {
			t.Fatalf("%s schema = type=%q notNull=%d default=%s, want type=%q notNull=%t default=%s", name, got.Type, got.NotNull, formatDefault(got.DefaultVal), want.typeName, want.notNull, formatDefault(want.default_))
		}
	}
}

func stringPtr(value string) *string { return &value }

func sameDefault(got, want *string) bool {
	if got == nil || want == nil {
		return got == want
	}
	return strings.Trim(*got, "'\"") == strings.Trim(*want, "'\"")
}

func formatDefault(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%q", *value)
}

func TestInvocationRepository_CompletedResultIsReusable(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-completed", "idem-completed")
	repo := magi.NewRuntimeInvocationRepository(db)
	ctx := context.Background()

	if _, won, err := repo.BeginAttempt(ctx, "invocation-completed", "attempt-1"); err != nil || !won {
		t.Fatalf("begin: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-completed", "attempt-1", `{"answer":42}`); err != nil || !won {
		t.Fatalf("complete: won=%v err=%v", won, err)
	}

	got, won, err := repo.BeginAttempt(ctx, "invocation-completed", "attempt-2")
	if err != nil || won {
		t.Fatalf("replay begin: invocation=%+v won=%v err=%v", got, won, err)
	}
	if got.Status != entity.InvocationSucceeded || got.OutputJSON != `{"answer":42}` || got.AttemptCount != 1 {
		t.Fatalf("completed result was not reusable: %+v", got)
	}
}

func TestInvocationRepository_LateAttemptCannotOverwriteNewAttempt(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-late", "idem-late")
	repo := magi.NewRuntimeInvocationRepository(db)
	ctx := context.Background()

	if _, won, err := repo.BeginAttempt(ctx, "invocation-late", "attempt-old"); err != nil || !won {
		t.Fatalf("begin old: won=%v err=%v", won, err)
	}
	if won, err := repo.Fail(ctx, "invocation-late", "attempt-old", "retryable"); err != nil || !won {
		t.Fatalf("fail old: won=%v err=%v", won, err)
	}
	if _, won, err := repo.BeginAttempt(ctx, "invocation-late", "attempt-new"); err != nil || !won {
		t.Fatalf("begin new: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-late", "attempt-old", `{"answer":"stale"}`); err != nil || won {
		t.Fatalf("late complete: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-late", "attempt-new", `{"answer":"fresh"}`); err != nil || !won {
		t.Fatalf("complete new: won=%v err=%v", won, err)
	}

	got, err := repo.Get(ctx, "invocation-late")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != entity.InvocationSucceeded || got.OutputJSON != `{"answer":"fresh"}` || got.AttemptCount != 2 {
		t.Fatalf("late attempt overwrote newer result: %+v", got)
	}
}

func TestInvocationRepository_IdempotencyKeyUnique(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-idempotency-1", "idem-unique")

	key := "idem-unique"
	err := db.Create(&magi.RuntimeInvocationModel{
		InvocationID:   "invocation-idempotency-2",
		RunID:          "run-1",
		StepID:         "step-2",
		Kind:           "tool",
		Status:         string(entity.InvocationPending),
		InputJSON:      `{}`,
		IdempotencyKey: &key,
	}).Error
	if err == nil {
		t.Fatal("duplicate idempotency key was accepted")
	}
}
