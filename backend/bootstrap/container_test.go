package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/bootstrap"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
	appserver "github.com/jamespud/magi/backend/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAllModels_MigrateWithoutError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("auto-migrate all models: %v", err)
	}
	for _, m := range magi.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("table for %T not created", m)
		}
	}
}

func TestAllModels_IncludesToolCallModel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	if !db.Migrator().HasTable("magi_tool_call") {
		t.Fatal("magi_tool_call table not created -- ToolCallModel missing from AllModels")
	}
}

func TestMigrationModels_ExcludeEventSequenceUntilAtlasMigration(t *testing.T) {
	models := magi.AllModelsWithoutEventSequence()
	if len(models) == 0 {
		t.Fatal("migration model list must not be empty")
	}
	for _, model := range models {
		switch model.(type) {
		case *magi.EventModel, *magi.EventCursorModel:
			t.Fatalf("production migration list must exclude event sequence models: %T", model)
		}
	}
}

func TestProvideToolRegistry_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolRegistry(withCfg, nil, magi.NewPluginAdapter(nil))
	if _, ok := with.(*magi.ToolRegistryMux); !ok {
		t.Fatalf("expected ToolRegistryMux when key set, got %T", with)
	}
	without := bootstrap.ProvideToolRegistry(&bootstrap.Config{}, nil, magi.NewPluginAdapter(nil))
	if _, ok := without.(*magi.ToolRegistryMux); !ok {
		t.Fatalf("expected ToolRegistryMux when no key, got %T", without)
	}
}

func TestProvideToolExecutor_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with, err := bootstrap.ProvideToolExecutor(withCfg, nil, metrics.New(), validation.NewJSONSchemaValidator(), nil, nil)
	if err != nil {
		t.Fatalf("provide with search: %v", err)
	}
	if _, ok := with.(*magi.ToolExecutorMux); !ok {
		t.Fatalf("expected ToolExecutorMux when key set, got %T", with)
	}
	without, err := bootstrap.ProvideToolExecutor(&bootstrap.Config{}, nil, metrics.New(), validation.NewJSONSchemaValidator(), nil, nil)
	if err != nil {
		t.Fatalf("provide without search: %v", err)
	}
	if _, ok := without.(*magi.ToolExecutorMux); !ok {
		t.Fatalf("expected ToolExecutorMux when no key, got %T", without)
	}
}

func sqliteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatal(err)
	}
	return db
}

type blockingOrch struct{}

func (blockingOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func a2aTestConfig(enabled bool) *bootstrap.Config {
	cfg := &bootstrap.Config{}
	cfg.A2A.Enabled = enabled
	cfg.A2A.PublicURL = "https://magi.example"
	cfg.A2A.BasePath = "/a2a"
	cfg.A2A.MaxMessageBytes = 65536
	cfg.A2A.MaxParts = 16
	cfg.A2A.MaxPageSize = 100
	cfg.A2A.MaxStreamsPerUserPerReplica = 8
	cfg.A2A.CrossInstancePollInterval = 2 * time.Second
	cfg.Magi.MaxDebateRounds = 3
	return cfg
}

func TestProvideA2A_DisabledReturnsInactiveWrapper(t *testing.T) {
	db := sqliteDB(t)
	cfg := a2aTestConfig(false)
	a2a := bootstrap.ProvideA2A(db, cfg, decision.NewRunManager(blockingOrch{}),
		appserver.NewEventBroker(), magi.NewRepository(db), metrics.New(), redact.New(), audit.NewService(nil))
	if a2a == nil || a2a.Enabled {
		t.Fatalf("disabled A2A wrapper = %+v", a2a)
	}
	if a2a.Handler != nil || a2a.SubmissionSvc != nil || a2a.StreamProjector != nil || a2a.MountDeps != nil {
		t.Fatalf("disabled A2A must not expose components: %+v", a2a)
	}
	if err := a2a.Recover(context.Background()); err != nil {
		t.Fatalf("disabled Recover = %v", err)
	}
}

func TestProvideA2A_EnabledResolvesAndRecoversPrepared(t *testing.T) {
	db := sqliteDB(t)
	cfg := a2aTestConfig(true)
	a2a := bootstrap.ProvideA2A(db, cfg, decision.NewRunManager(blockingOrch{}),
		appserver.NewEventBroker(), magi.NewRepository(db), metrics.New(), redact.New(), audit.NewService(nil))
	if a2a == nil || !a2a.Enabled {
		t.Fatalf("enabled A2A wrapper = %+v", a2a)
	}
	if a2a.SubmissionRepo == nil || a2a.SubmissionSvc == nil || a2a.TaskProjector == nil ||
		a2a.StreamProjector == nil || a2a.MountDeps == nil {
		t.Fatalf("enabled A2A missing components: %+v", a2a)
	}
	var _ a2asrv.RequestHandler = a2a.Handler

	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := a2a.SubmissionRepo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := a2a.Recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	sub, err := a2a.SubmissionRepo.GetByTask(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding state = %s, want STARTED after lifecycle recovery", sub.State)
	}
}

// TestA2A_StartRecoveryWorkerStartsAndStops guards the lifecycle wiring: the
// recovery worker starts for an enabled bundle and stops cleanly on cancel,
// while a disabled bundle returns a no-op cancel.
func TestA2A_StartRecoveryWorkerStartsAndStops(t *testing.T) {
	db := sqliteDB(t)
	cfg := a2aTestConfig(true)
	enabled := bootstrap.ProvideA2A(db, cfg, decision.NewRunManager(blockingOrch{}),
		appserver.NewEventBroker(), magi.NewRepository(db), metrics.New(), redact.New(), audit.NewService(nil))
	cancel := enabled.StartRecoveryWorker(context.Background())
	cancel()

	disabled := bootstrap.ProvideA2A(db, a2aTestConfig(false), decision.NewRunManager(blockingOrch{}),
		appserver.NewEventBroker(), magi.NewRepository(db), metrics.New(), redact.New(), audit.NewService(nil))
	noop := disabled.StartRecoveryWorker(context.Background())
	noop()
}

func TestProvideKnowledgePort_ReturnsNonNil(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := &bootstrap.Config{}
	cfg.Embedding.Dim = 3
	// Empty Milvus/ES addresses -> fake indexes; no real connections.
	kp, idx, mem, err := bootstrap.ProvideKnowledgePort(cfg, db, nil)
	if err != nil {
		t.Fatalf("ProvideKnowledgePort: %v", err)
	}
	if kp == nil {
		t.Error("expected non-nil KnowledgePort")
	}
	if idx == nil {
		t.Error("expected non-nil DocumentIndexer")
	}
	if mem == nil {
		t.Error("expected non-nil MemoryIndexer")
	}
}

func TestProvideKnowledgePort_AsyncReturnsMemoryIndexer(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := &bootstrap.Config{}
	cfg.Embedding.Dim = 3
	cfg.RAG.StoreAsync = true
	kp, doc, mem, err := bootstrap.ProvideKnowledgePort(cfg, db, nil)
	if err != nil {
		t.Fatalf("ProvideKnowledgePort: %v", err)
	}
	if kp == nil || doc == nil || mem == nil {
		t.Fatalf("nil interface in async mode: kp=%v doc=%v mem=%v", kp == nil, doc == nil, mem == nil)
	}
}
