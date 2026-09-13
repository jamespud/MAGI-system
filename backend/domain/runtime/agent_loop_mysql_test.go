package runtime_test

import (
	"context"
	"os"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/modelruntime"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

func openRuntimeMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("MAGI_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration tests — a green run without it does NOT mean MySQL was verified")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	if err := db.AutoMigrate(&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}); err != nil {
		t.Fatalf("migrate runtime invocation tables: %v", err)
	}
	return db
}

// TestAgentLoop_PersistsProductionLengthIdentityOnMySQL drives the real runtime
// path — agent loop -> model invocation -> execution kernel -> MySQL — with the
// case-id shape production uses.
//
// Before the identity columns were widened this path failed with
// `Error 1406 (22001): Data too long for column 'run_id'` and the run never
// completed: SQLite ignores VARCHAR widths and the existing Go tests used short
// ids, so only a real case id against real MySQL exposed it. A repository-level
// insert test cannot catch a regression here, which is why this drives the loop.
func TestAgentLoop_PersistsProductionLengthIdentityOnMySQL(t *testing.T) {
	db := openRuntimeMySQL(t)
	repo := magi.NewRuntimeInvocationRepository(db)
	mrt := modelruntime.New(execution.NewKernel(repo, nil))
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		// No tools bound, so the flow is summary -> vote with a relaxed gate.
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			finalMsg(summaryJSON()),
			finalMsg(voteJSON("safety")),
		}}},
		ModelRuntime: mrt,
		Invocations:  repo,
		Validator:    val,
		Gen:          gen,
		Gate:         evidence.NewEvidenceGate(),
	})
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}

	// uuid.NewString() is 36 characters, so this is exactly the production
	// "case-<uuid>" shape (41 chars) while staying unique per run.
	caseID := "case-" + uuid.NewString()
	runID := caseID + "-balthasar-a2-r1-investigate" // 69 chars: the dispatcher's worst case
	if len(caseID) != 41 || len(runID) != 69 {
		t.Fatalf("fixture ids = %d/%d chars, want 41/69", len(caseID), len(runID))
	}
	cfg := &entity.MagiConfig{
		Code:         "balthasar",
		Persona:      "protector",
		RiskTendency: entity.RiskTendencyNeutral,
		Objective: entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{
			{Code: "safety", Weight: 1, Description: "be safe"},
		}},
		Model:      entity.ModelRef{ModelID: 1},
		LoopPolicy: entity.LoopPolicy{MaxSteps: 4},
	}

	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{
		CaseID: caseID,
		RunID:  runID,
		Task:   entity.DecisionTask{CanonicalQuestion: "Should the fixture adopt the change?"},
	})
	if err != nil {
		t.Fatalf("run with %d-char run id: %v", len(runID), err)
	}
	if res.Status != runtime.LoopStatusCompleted || res.Vote == nil {
		t.Fatalf("loop result = %+v, want a completed vote", res)
	}

	// The long identity must have reached MySQL, not just the loop's memory.
	var invocations int64
	if err := db.Model(&magi.RuntimeInvocationModel{}).Where("run_id = ?", runID).Count(&invocations).Error; err != nil {
		t.Fatalf("count invocations: %v", err)
	}
	if invocations == 0 {
		t.Fatalf("no runtime_invocation row stored with the %d-char run id", len(runID))
	}
	// One attempt row per invocation, each scoped to its invocation: the run
	// attempt id alone is shared by every invocation and would collide on the
	// attempt primary key.
	var attempts int64
	if err := db.Model(&magi.RuntimeInvocationAttemptModel{}).
		Where("attempt_id LIKE ?", runID+":%").Count(&attempts).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != invocations {
		t.Fatalf("attempt rows = %d but invocation rows = %d, want one attempt per invocation", attempts, invocations)
	}
	var unscoped int64
	if err := db.Model(&magi.RuntimeInvocationAttemptModel{}).
		Where("attempt_id = ?", runID).Count(&unscoped).Error; err != nil {
		t.Fatalf("count unscoped attempts: %v", err)
	}
	if unscoped != 0 {
		t.Fatalf("attempt rows must be scoped per invocation, found %d using the bare run id", unscoped)
	}
}
