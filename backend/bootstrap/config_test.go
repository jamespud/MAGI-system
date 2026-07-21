package bootstrap_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamespud/magi/backend/bootstrap"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "test-key"
  base_url: "http://localhost"
  model_name: "test-model"
magi:
  melchior:
    persona: "test"
`), 0644)

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Magi.MaxDebateRounds != 2 {
		t.Fatalf("MaxDebateRounds default: got %d want 2", cfg.Magi.MaxDebateRounds)
	}
	if cfg.Magi.MaxSteps != 12 {
		t.Fatalf("MaxSteps default: got %d want 12", cfg.Magi.MaxSteps)
	}
	if cfg.Magi.TimeoutSeconds != 120 {
		t.Fatalf("TimeoutSeconds default: got %d want 120", cfg.Magi.TimeoutSeconds)
	}
	if cfg.Model.APIKey != "test-key" {
		t.Fatalf("APIKey: got %s", cfg.Model.APIKey)
	}
}

func TestLoadConfig_Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "test-key"
  base_url: "http://localhost"
  model_name: "test-model"
magi:
  melchior:
    persona: "test"
database:
  driver: mysql
  dsn: "user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8mb4&parseTime=True"
`), 0644)

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Database.Driver != "mysql" {
		t.Fatalf("Database.Driver: got %q want mysql", cfg.Database.Driver)
	}
	if cfg.Database.DSN != "user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8mb4&parseTime=True" {
		t.Fatalf("Database.DSN: got %q", cfg.Database.DSN)
	}
}

func TestConfig_TavilyParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "k"
tavily:
  api_key: "tvly-test-key"
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tavily.APIKey != "tvly-test-key" {
		t.Fatalf("Tavily.APIKey: got %q", cfg.Tavily.APIKey)
	}
}

func TestMagiSpec_ToConfigBindsWebSearch(t *testing.T) {
	cfg := &bootstrap.Config{}
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	if len(c.Tools) != 1 || c.Tools[0].ToolName != "web_search" {
		t.Fatalf("expected web_search bound, got %+v", c.Tools)
	}
	if c.Tools[0].Source != entity.ToolSourceLocal {
		t.Fatalf("expected local source, got %s", c.Tools[0].Source)
	}
}
