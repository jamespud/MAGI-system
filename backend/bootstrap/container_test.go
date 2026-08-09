package bootstrap_test

import (
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/bootstrap"
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

func TestProvideToolRegistry_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolRegistry(withCfg, nil)
	if _, ok := with.(*magi.ToolRegistryMux); !ok {
		t.Fatalf("expected ToolRegistryMux when key set, got %T", with)
	}
	without := bootstrap.ProvideToolRegistry(&bootstrap.Config{}, nil)
	if _, ok := without.(*magi.ToolRegistryMux); !ok {
		t.Fatalf("expected ToolRegistryMux when no key, got %T", without)
	}
}

func TestProvideToolExecutor_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolExecutor(withCfg, nil)
	if _, ok := with.(*magi.ToolExecutorMux); !ok {
		t.Fatalf("expected ToolExecutorMux when key set, got %T", with)
	}
	without := bootstrap.ProvideToolExecutor(&bootstrap.Config{}, nil)
	if _, ok := without.(*magi.ToolExecutorMux); !ok {
		t.Fatalf("expected ToolExecutorMux when no key, got %T", without)
	}
}

func TestProvideKnowledgePort_ReturnsNonNil(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := &bootstrap.Config{}
	cfg.Embedding.Dim = 3
	// Empty Milvus/ES addresses -> fake indexes; no real connections.
	kp, err := bootstrap.ProvideKnowledgePort(cfg, db, nil)
	if err != nil {
		t.Fatalf("ProvideKnowledgePort: %v", err)
	}
	if kp == nil {
		t.Error("expected non-nil KnowledgePort")
	}
}
