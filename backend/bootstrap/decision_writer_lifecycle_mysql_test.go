package bootstrap

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

func TestMySQLDecisionWriter_LifecycleRefusesBeforeLaunch(t *testing.T) {
	for _, failure := range []string{"schema", "gate"} {
		t.Run(failure, func(t *testing.T) {
			dsn, db := t6SchemaFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
			if err := magi.NewRepository(db).CaseRepo().Create(ctx, c); err != nil {
				t.Fatal(err)
			}
			jobs := magi.NewDecisionJobRepository(db)
			if _, _, err := jobs.Admit(ctx, c.ID, 3, 0); err != nil {
				t.Fatal(err)
			}
			if failure == "schema" {
				if err := db.Exec("ALTER TABLE magi_vote DROP COLUMN execution_generation").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := BlockDecisionWriter(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			before := t6Snapshot(t, db)
			cfg := &Config{}
			cfg.Database.DSN = dsn
			cfg.Database.LogLevel = "silent"
			cfg.Benchmark.AutoIntervalSeconds = 1
			constructed := false
			app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(provideDB),
				fx.Invoke(func(lc fx.Lifecycle, runtimeDB *gorm.DB) {
					constructed = true
					repo := magi.NewDecisionWriterRepository(runtimeDB)
					liveJobs := magi.NewDecisionJobRepository(runtimeDB)
					orch := &t6OwnedOrchestrator{repo: repo, called: make(chan *entity.ExecutionContext, 10)}
					rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: liveJobs, CaseRepo: repo.CaseRepo(), Metrics: metrics.New()})
					ds := provideDatasetService(magi.NewDatasetRepository(runtimeDB), rm, liveJobs, repo, cfg, metrics.New())
					registerLifecycle(lc, rm, ds, nil, nil, cfg, &A2A{}, metrics.New())
					registerScheduler(lc, recurring.NewService(magi.NewRecurringRepository(runtimeDB), repo.CaseRepo(), rm, 1), magi.NewSchedulerLock(runtimeDB), rm)
				}))
			if err := app.Start(ctx); err == nil {
				t.Fatal("runtime started while capability invalid")
			}
			if constructed {
				t.Fatal("execution services constructed before writer verifier")
			}
			if !reflect.DeepEqual(before, t6Snapshot(t, db)) {
				t.Fatal("startup refusal changed authoritative data")
			}
			var count int64
			db.Model(&magi.BenchmarkRunModel{}).Count(&count)
			if count != 0 {
				t.Fatal("startup launched benchmark")
			}
		})
	}
}

func TestMySQLDecisionWriter_DatasetAndSchedulerBlocked(t *testing.T) {
	_, db := t6SchemaFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := magi.NewDecisionWriterRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &t6OwnedOrchestrator{repo: repo, called: make(chan *entity.ExecutionContext, 1)}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: repo.(port.OwnedCaseCommitter), Metrics: metrics.New()})
	defer rm.Shutdown()
	datasets := magi.NewDatasetRepository(db)
	cfg := &Config{}
	cfg.Benchmark.RunsPerItem = 1
	ds := provideDatasetService(datasets, rm, jobs, repo, cfg, metrics.New())
	seed, err := ds.Create(ctx, 0, "t6 benchmark", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ds.AddItems(ctx, 0, seed.ID, []dataset.NewItem{{Question: "approve?", ExpectedDecision: entity.VoteDecisionApprove, Weight: 1}}); err != nil {
		t.Fatal(err)
	}
	// Validate the actual Dataset service/provider, not only its executor adapter.
	run, err := ds.StartRun(ctx, 0, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := ds.AwaitRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != entity.BenchmarkRunSucceeded {
		t.Fatalf("benchmark failed: %+v", completed)
	}
	results, err := datasets.ListItemResults(ctx, run.ID)
	if err != nil || len(results) != 1 {
		t.Fatal("missing benchmark result")
	}
	c, err := repo.CaseRepo().Get(ctx, results[0].CaseID)
	if err != nil || c.ExecutionGeneration != 1 || c.Status != entity.CaseStatusResolved {
		t.Fatal("Dataset left generation-zero terminal")
	}
	<-orch.called
	if _, err := BlockDecisionWriter(ctx, db); err != nil {
		t.Fatal(err)
	}
	before := t6Snapshot(t, db)
	if _, err := ds.StartRun(ctx, 0, seed.ID); !errors.Is(err, port.ErrDecisionWriterBlocked) {
		t.Fatalf("benchmark admitted with closed gate: %v", err)
	}
	if err := ds.RecoverOrphanRuns(ctx); !errors.Is(err, port.ErrDecisionWriterBlocked) {
		t.Fatal("benchmark recovery ran with closed gate")
	}
	// Scheduler's actual OnStart hook must reject even when a preconstructed
	// runtime loses admission between DB verification and lifecycle startup.
	app := fx.New(fx.NopLogger, fx.Invoke(func(lc fx.Lifecycle) {
		registerScheduler(lc, recurring.NewService(magi.NewRecurringRepository(db), repo.CaseRepo(), rm, 1), magi.NewSchedulerLock(db), rm)
	}))
	if err := app.Start(ctx); !errors.Is(err, port.ErrDecisionWriterBlocked) {
		t.Fatalf("scheduler startup accepted closed gate: %v", err)
	}
	// Explicit legacy dataset configuration fails before creating a benchmark.
	ownerless := dataset.NewService(datasets, repo.CaseRepo(), orchestration.NewOrchestrator(orchestration.OrchestratorDeps{Repo: repo}), 1)
	if _, err := ownerless.StartRun(ctx, 0, seed.ID); !errors.Is(err, port.ErrExecutionOwnerRequired) {
		t.Fatal("ownerless dataset started")
	}
	if !reflect.DeepEqual(before, t6Snapshot(t, db)) {
		t.Fatal("blocked launch changed Case/Job/Artifact/Event")
	}
	runs, err := datasets.ListRuns(ctx, seed.ID)
	if err != nil || len(runs) != 1 {
		t.Fatal("blocked benchmark persisted a new run")
	}
}
