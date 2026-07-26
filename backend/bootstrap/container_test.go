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
	with := bootstrap.ProvideToolRegistry(withCfg)
	if _, ok := with.(*magi.LocalToolRegistry); !ok {
		t.Fatalf("expected LocalToolRegistry when key set, got %T", with)
	}
	without := bootstrap.ProvideToolRegistry(&bootstrap.Config{})
	if _, ok := without.(*bootstrap.StubToolRegistry); !ok {
		t.Fatalf("expected StubToolRegistry when no key, got %T", without)
	}
}

func TestProvideToolExecutor_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolExecutor(withCfg)
	if _, ok := with.(*magi.TavilyToolExecutor); !ok {
		t.Fatalf("expected TavilyToolExecutor when key set, got %T", with)
	}
	without := bootstrap.ProvideToolExecutor(&bootstrap.Config{})
	if _, ok := without.(*bootstrap.StubToolExecutor); !ok {
		t.Fatalf("expected StubToolExecutor when no key, got %T", without)
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
	kp, err := bootstrap.ProvideKnowledgePort(cfg, db)
	if err != nil {
		t.Fatalf("ProvideKnowledgePort: %v", err)
	}
	if kp == nil {
		t.Error("expected non-nil KnowledgePort")
	}
}
