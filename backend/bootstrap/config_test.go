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

func TestMagiSpec_ToConfigEnablesRoleContract(t *testing.T) {
	cfg := &bootstrap.Config{}
	melchior := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	balthasar := cfg.Magi.Balthasar.ToConfig("balthasar", cfg)
	casper := cfg.Magi.Casper.ToConfig("casper", cfg)
	if !melchior.RolePolicy.EnforceAssessment || melchior.RolePolicy.RequiredAssessment != entity.RoleAssessmentTechnical {
		t.Fatalf("Melchior role policy: %+v", melchior.RolePolicy)
	}
	if balthasar.RolePolicy.RequiredAssessment != entity.RoleAssessmentRisk || balthasar.RolePolicy.MaxResidualRisk != 0.35 {
		t.Fatalf("Balthasar role policy: %+v", balthasar.RolePolicy)
	}
	if casper.RolePolicy.RequiredAssessment != entity.RoleAssessmentOpportunity || casper.RolePolicy.MinOpportunityScore != 60 {
		t.Fatalf("Casper role policy: %+v", casper.RolePolicy)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "yaml-key"
  base_url: "http://yaml"
  model_name: "yaml-model"
database:
  driver: mysql
  dsn: "yaml-dsn"
tavily:
  api_key: "yaml-tavily"
`), 0644)

	t.Setenv("MAGI_DB_DSN", "env-dsn")
	t.Setenv("MAGI_DB_DRIVER", "env-driver")
	t.Setenv("MAGI_MODEL_API_KEY", "env-key")
	t.Setenv("MAGI_MODEL_BASE_URL", "http://env")
	t.Setenv("MAGI_MODEL_NAME", "env-model")
	t.Setenv("MAGI_TAVILY_API_KEY", "env-tavily")

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Database.DSN != "env-dsn" {
		t.Fatalf("DSN: got %q want env-dsn", cfg.Database.DSN)
	}
	if cfg.Database.Driver != "env-driver" {
		t.Fatalf("Driver: got %q want env-driver", cfg.Database.Driver)
	}
	if cfg.Model.APIKey != "env-key" {
		t.Fatalf("Model.APIKey: got %q want env-key", cfg.Model.APIKey)
	}
	if cfg.Model.BaseURL != "http://env" {
		t.Fatalf("Model.BaseURL: got %q want http://env", cfg.Model.BaseURL)
	}
	if cfg.Model.ModelName != "env-model" {
		t.Fatalf("Model.ModelName: got %q want env-model", cfg.Model.ModelName)
	}
	if cfg.Tavily.APIKey != "env-tavily" {
		t.Fatalf("Tavily.APIKey: got %q want env-tavily", cfg.Tavily.APIKey)
	}
}

func TestLoadConfig_EnvEmptyKeepsYAML(t *testing.T) {
	t.Setenv("MAGI_DB_DSN", "")
	t.Setenv("MAGI_MODEL_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "yaml-key"
database:
  dsn: "yaml-dsn"
`), 0644)

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Database.DSN != "yaml-dsn" {
		t.Fatalf("DSN: got %q want yaml-dsn", cfg.Database.DSN)
	}
	if cfg.Model.APIKey != "yaml-key" {
		t.Fatalf("Model.APIKey: got %q want yaml-key", cfg.Model.APIKey)
	}
}
