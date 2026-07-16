package bootstrap_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamespud/magi/backend/bootstrap"
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
