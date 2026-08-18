package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/bootstrap"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestConfigValidate_ExamplePasses(t *testing.T) {
	cfg, err := bootstrap.LoadConfig("../conf/magi.yaml.example")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate example: %v", err)
	}
}

func TestConfigValidate_RejectsBrokenConfigs(t *testing.T) {
	cfg := &bootstrap.Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing model")
	}

	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Auth.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for enabled auth without keys")
	}
	cfg.Auth.APIKeys = []bootstrap.APIKeySpec{{Key: "", UserID: 1, Role: "admin"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty auth key")
	}
}

func TestConfigValidate_MCPRejectsInvalidServers(t *testing.T) {
	base := func() *bootstrap.Config {
		dir := t.TempDir()
		path := filepath.Join(dir, "base.yaml")
		os.WriteFile(path, []byte(`
model:
  api_key: "k"
  model_name: "m"
magi:
  max_debate_rounds: 1
  max_steps: 1
  timeout_seconds: 1
  call_timeout_seconds: 1
`), 0644)
		cfg, err := bootstrap.LoadConfig(path)
		if err != nil {
			t.Fatalf("load base config: %v", err)
		}
		return cfg
	}

	cases := []struct {
		name    string
		servers []bootstrap.MCPServerConfig
		wantErr string
	}{
		{"missing name", []bootstrap.MCPServerConfig{{Transport: "stdio", Command: "x"}}, "name is required"},
		{"duplicate name", []bootstrap.MCPServerConfig{{Name: "a", Transport: "stdio", Command: "x"}, {Name: "a", Transport: "stdio", Command: "y"}}, "duplicate server name"},
		{"bad transport", []bootstrap.MCPServerConfig{{Name: "a", Transport: "carrier-pigeon"}}, "transport must be"},
		{"stdio without command", []bootstrap.MCPServerConfig{{Name: "a", Transport: "stdio"}}, "requires command"},
		{"http without url", []bootstrap.MCPServerConfig{{Name: "a", Transport: "http"}}, "requires url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.MCP.Servers = tc.servers
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestConfigValidate_MCPAcceptsValidServers(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1
	cfg.MCP.Servers = []bootstrap.MCPServerConfig{
		{Name: "local", Transport: "stdio", Command: "/bin/echo"},
		{Name: "remote", Transport: "http", URL: "https://mcp.example.com/mcp"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestLoadConfig_MCPAndCodeRunnerDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "test-key"
  base_url: "http://localhost"
  model_name: "test-model"
mcp:
  servers:
    - { name: "s", transport: "stdio", command: "/bin/echo" }
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CodeRunner.MemoryLimitMB != 100 {
		t.Errorf("MemoryLimitMB = %d, want 100", cfg.CodeRunner.MemoryLimitMB)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].TimeoutSeconds != 60 {
		t.Errorf("MCP servers = %+v, want timeout 60", cfg.MCP.Servers)
	}
}

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
	if cfg.Magi.TimeoutSeconds != 600 {
		t.Fatalf("TimeoutSeconds default: got %d want 600", cfg.Magi.TimeoutSeconds)
	}
	if cfg.Magi.CallTimeoutSeconds != 180 {
		t.Fatalf("CallTimeoutSeconds default: got %d want 180", cfg.Magi.CallTimeoutSeconds)
	}
	if cfg.FeedbackTool.Enabled == nil || !*cfg.FeedbackTool.Enabled {
		t.Fatal("feedback_tool should default to enabled")
	}
	if cfg.Model.APIKey != "test-key" {
		t.Fatalf("APIKey: got %s", cfg.Model.APIKey)
	}
}

func TestLoadConfig_FeedbackToolExplicitDisable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "k"
  model_name: "m"
feedback_tool:
  enabled: false
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.FeedbackTool.Enabled == nil || *cfg.FeedbackTool.Enabled {
		t.Fatal("feedback_tool.enabled=false must be respected")
	}
}

func TestMagiSpec_ToConfigBindsFeedbackToolWhenEnabled(t *testing.T) {
	enabled := true
	cfg := &bootstrap.Config{FeedbackTool: bootstrap.FeedbackToolConfig{Enabled: &enabled}}
	cfg.DBTool.Enabled = true
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := map[string]bool{}
	for _, tool := range c.Tools {
		found[tool.ToolName] = true
	}
	if !found["check_output"] {
		t.Fatalf("check_output not bound when feedback_tool enabled: %+v", c.Tools)
	}
	if !found["db_query"] || !found["web_search"] {
		t.Fatalf("existing local tools must remain bound: %+v", c.Tools)
	}

	disabled := false
	cfg2 := &bootstrap.Config{FeedbackTool: bootstrap.FeedbackToolConfig{Enabled: &disabled}}
	c2 := cfg2.Magi.Melchior.ToConfig("melchior", cfg2)
	for _, tool := range c2.Tools {
		if tool.ToolName == "check_output" {
			t.Fatal("check_output must not be bound when disabled")
		}
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

func TestLoadConfig_SearchProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "search.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "k"
  model_name: "m"
search:
  providers:
    - provider: "tavily"
      api_key: "tavily-key"
    - provider: "brave"
      api_key: "brave-key"
      base_url: "https://search.internal/api"
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(cfg.Search.Providers) != 2 || cfg.Search.Providers[0].Provider != "tavily" ||
		cfg.Search.Providers[1].Provider != "brave" || cfg.Search.Providers[1].BaseURL != "https://search.internal/api" {
		t.Fatalf("search providers = %+v", cfg.Search.Providers)
	}
}

func TestConfigValidate_SearchProviders(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1
	cfg.Search.Providers = []bootstrap.SearchProviderConfig{{Provider: "google"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
	cfg.Search.Providers = []bootstrap.SearchProviderConfig{
		{Provider: "brave", APIKey: "one"},
		{Provider: "brave", APIKey: "two"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate provider") {
		t.Fatalf("expected duplicate provider error, got %v", err)
	}
	cfg.Search.Providers = []bootstrap.SearchProviderConfig{{Provider: "brave"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("known keyless provider should be a disabled placeholder: %v", err)
	}
}

func TestLoadConfig_DBTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db-tool.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "k"
  model_name: "m"
db_tool:
  enabled: true
  driver: "sqlite3"
  dsn: "/tmp/magi.db"
  max_rows: 25
  max_query_chars: 500
  timeout_seconds: 3
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !cfg.DBTool.Enabled || cfg.DBTool.Driver != "sqlite3" || cfg.DBTool.MaxRows != 25 {
		t.Fatalf("db_tool config = %+v", cfg.DBTool)
	}
}

func TestConfigValidate_DBTool(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.DBTool.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "db_tool") {
		t.Fatalf("expected db_tool validation error, got %v", err)
	}

	cfg.DBTool.Driver = "oracle"
	cfg.DBTool.DSN = "x"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `driver must be "mysql" or "sqlite3"`) {
		t.Fatalf("expected driver validation error, got %v", err)
	}

	cfg.DBTool.Driver = "sqlite3"
	cfg.DBTool.DSN = "/tmp/magi.db"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid db_tool rejected: %v", err)
	}
}

func TestConfigValidate_DockerCodeRunner(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.CodeRunner.Docker.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "code_runner.docker: image is required") {
		t.Fatalf("expected docker image validation error, got %v", err)
	}
	cfg.CodeRunner.Docker.Image = "python:3.12-slim"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid docker config rejected: %v", err)
	}
}

func TestConfigValidate_FileTool(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.FileTool.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "file_tool: at least one root") {
		t.Fatalf("expected file_tool roots validation error, got %v", err)
	}
	cfg.FileTool.Roots = []string{"/tmp"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid file_tool rejected: %v", err)
	}
}

func TestConfigValidate_RepoTool(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.RepoTool.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "repo_tool: at least one root") {
		t.Fatalf("expected repo_tool roots validation error, got %v", err)
	}
	cfg.RepoTool.Roots = []string{"/srv/repo"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid repo_tool rejected: %v", err)
	}
}

func TestConfigValidate_WebTool(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.WebTool.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "web_tool: at least one allowed domain") {
		t.Fatalf("expected web_tool domains validation error, got %v", err)
	}
	cfg.WebTool.AllowedDomains = []string{"example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid web_tool rejected: %v", err)
	}
}

func TestConfigValidate_OIDC(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.Auth.OIDC.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "auth.oidc: issuer, client_id and redirect_url") {
		t.Fatalf("expected oidc validation error, got %v", err)
	}
	cfg.Auth.OIDC.Issuer = "https://issuer.example"
	cfg.Auth.OIDC.ClientID = "cid"
	cfg.Auth.OIDC.RedirectURL = "http://localhost/auth/oidc/callback"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "session_secret is required") {
		t.Fatalf("expected session_secret error, got %v", err)
	}
	cfg.Auth.OIDC.SessionSecret = "s3cret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid oidc rejected: %v", err)
	}
}

func TestConfigValidate_SelfImproveAutoApply(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.SelfImprove.AutoApplyEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "selfimprove: auto_apply_threshold") {
		t.Fatalf("expected threshold validation error, got %v", err)
	}
	cfg.SelfImprove.AutoApplyThreshold = 2
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid selfimprove rejected: %v", err)
	}
}

func TestMagiSpec_ToConfigBindsDelegateToolWhenEnabled(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.DelegateTool.Enabled = true
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := false
	for _, tool := range c.Tools {
		if tool.ToolName == "delegate" && tool.Source == entity.ToolSourceLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("delegate not bound when delegate_tool enabled: %+v", c.Tools)
	}

	disabled := &bootstrap.Config{}
	c2 := disabled.Magi.Melchior.ToConfig("melchior", disabled)
	for _, tool := range c2.Tools {
		if tool.ToolName == "delegate" {
			t.Fatal("delegate must not be bound when disabled")
		}
	}
}

func TestMagiSpec_ToConfigBindsWebToolWhenEnabled(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.WebTool.Enabled = true
	cfg.WebTool.AllowedDomains = []string{"example.com"}
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := false
	for _, tool := range c.Tools {
		if tool.ToolName == "web_fetch" && tool.Source == entity.ToolSourceLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("web_fetch not bound when web_tool enabled: %+v", c.Tools)
	}
}

func TestMagiSpec_ToConfigBindsRepoToolWhenEnabled(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.RepoTool.Enabled = true
	cfg.RepoTool.Roots = []string{"/srv/repo"}
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := false
	for _, tool := range c.Tools {
		if tool.ToolName == "repo_query" && tool.Source == entity.ToolSourceLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("repo_query not bound when repo_tool enabled: %+v", c.Tools)
	}
}

func TestMagiSpec_ToConfigBindsFileToolWhenEnabled(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.FileTool.Enabled = true
	cfg.FileTool.Roots = []string{"/tmp"}
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := false
	for _, tool := range c.Tools {
		if tool.ToolName == "file_query" && tool.Source == entity.ToolSourceLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("file_query not bound when file_tool enabled: %+v", c.Tools)
	}
}

func TestMagiSpec_ToConfigBindsDBQueryWhenEnabled(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.DBTool.Enabled = true
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	found := false
	for _, tool := range c.Tools {
		if tool.ToolName == "db_query" && tool.Source == entity.ToolSourceLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("db_query not bound when db_tool enabled: %+v", c.Tools)
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

func TestLoadConfig_RAGDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "k"
magi:
  melchior:
    persona: "test"
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RAG.TopK != 15 {
		t.Errorf("RAG.TopK = %d, want 15", cfg.RAG.TopK)
	}
	if cfg.RAG.RRFK != 60 {
		t.Errorf("RAG.RRFK = %d, want 60", cfg.RAG.RRFK)
	}
	if cfg.RAG.MergeThreshold900 != 3 {
		t.Errorf("MergeThreshold900 = %d, want 3", cfg.RAG.MergeThreshold900)
	}
	if cfg.RAG.MergeThreshold1800 != 2 {
		t.Errorf("MergeThreshold1800 = %d, want 2", cfg.RAG.MergeThreshold1800)
	}
	if cfg.RAG.OrphanStrategy != "keep_300" {
		t.Errorf("OrphanStrategy = %q, want keep_300", cfg.RAG.OrphanStrategy)
	}
	if cfg.RAG.StoreWorkers != 4 {
		t.Errorf("StoreWorkers = %d, want 4", cfg.RAG.StoreWorkers)
	}
	if len(cfg.RAG.Levels) != 3 || cfg.RAG.Levels[0] != 1800 || cfg.RAG.Levels[2] != 300 {
		t.Errorf("Levels = %v, want [1800 900 300]", cfg.RAG.Levels)
	}
}

func TestLoadConfig_RAGEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "yaml-key"
embedding:
  base_url: "http://yaml-emb"
  model_name: "bge-m3"
  dim: 1024
milvus:
  address: "yaml:19530"
  collection: "yaml_coll"
elasticsearch:
  addresses: ["http://yaml:9200"]
  index: "yaml_idx"
`), 0644)

	t.Setenv("MAGI_EMBEDDING_API_KEY", "env-emb-key")
	t.Setenv("MAGI_MILVUS_ADDRESS", "env:19530")
	t.Setenv("MAGI_ES_ADDRESSES", "http://env:9200")

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Embedding.APIKey != "env-emb-key" {
		t.Errorf("Embedding.APIKey = %q, want env-emb-key", cfg.Embedding.APIKey)
	}
	if cfg.Embedding.BaseURL != "http://yaml-emb" {
		t.Errorf("Embedding.BaseURL = %q, want http://yaml-emb", cfg.Embedding.BaseURL)
	}
	if cfg.Embedding.Dim != 1024 {
		t.Errorf("Embedding.Dim = %d, want 1024", cfg.Embedding.Dim)
	}
	if cfg.Milvus.Address != "env:19530" {
		t.Errorf("Milvus.Address = %q, want env:19530", cfg.Milvus.Address)
	}
	if len(cfg.Elasticsearch.Addresses) != 1 || cfg.Elasticsearch.Addresses[0] != "http://env:9200" {
		t.Errorf("ES.Addresses = %v, want [http://env:9200]", cfg.Elasticsearch.Addresses)
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

func TestLoadConfig_PerRoleModelOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roles.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "global-key"
  base_url: "https://global.example.com"
  model_name: "global-model"
magi:
  max_debate_rounds: 1
  max_steps: 1
  timeout_seconds: 1
  call_timeout_seconds: 1
  melchior:
    model:
      api_key: "mel-key"
      base_url: "https://mel.example.com"
      model_name: "mel-model"
  casper:
    model:
      model_id: 99
      price_per_m_input_usd: 4.0
commander:
  model:
    model_name: "commander-model"
judge:
  model:
    base_url: "https://judge.example.com"
    model_name: "judge-model"
`), 0644)

	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	mel := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	if mel.Model.APIKey != "mel-key" || mel.Model.BaseURL != "https://mel.example.com" || mel.Model.ModelName != "mel-model" {
		t.Fatalf("melchior model override wrong: %+v", mel.Model)
	}
	if mel.Model.ModelID != 0 || mel.Model.PricePerMInputUSD != 2.5 {
		t.Fatalf("melchior should inherit default price/coze-id: %+v", mel.Model)
	}

	bal := cfg.Magi.Balthasar.ToConfig("balthasar", cfg)
	if bal.Model.APIKey != "global-key" || bal.Model.ModelName != "global-model" || bal.Model.BaseURL != "https://global.example.com" {
		t.Fatalf("balthasar should fall back to global model: %+v", bal.Model)
	}

	cas := cfg.Magi.Casper.ToConfig("casper", cfg)
	if cas.Model.ModelID != 99 || cas.Model.PricePerMInputUSD != 4.0 {
		t.Fatalf("casper coze-mode override wrong: %+v", cas.Model)
	}
	if cas.Model.APIKey != "global-key" || cas.Model.ModelName != "global-model" {
		t.Fatalf("casper should inherit api_key/model_name from global: %+v", cas.Model)
	}

	if cm := cfg.CommanderModelRef(); cm.ModelName != "commander-model" || cm.APIKey != "global-key" || cm.BaseURL != "https://global.example.com" {
		t.Fatalf("commander override wrong: %+v", cm)
	}
	if j := cfg.JudgeModelRef(); j.ModelName != "judge-model" || j.BaseURL != "https://judge.example.com" || j.APIKey != "global-key" {
		t.Fatalf("judge override wrong: %+v", j)
	}
}

func TestLoadConfig_ModelProviderFailoverChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-failover.yaml")
	os.WriteFile(path, []byte(`
model:
  api_key: "global-key"
  base_url: "https://global.example.com"
  model_name: "global-model"
  providers:
    - api_key: "fallback-key"
      base_url: "https://fallback.example.com"
      model_name: "fallback-model"
magi:
  melchior:
    model:
      base_url: "https://role.example.com"
      model_name: "role-model"
      providers:
        - base_url: "https://role-fallback.example.com"
          model_name: "role-fallback-model"
`), 0644)
	cfg, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	globalUser := cfg.Magi.Balthasar.ToConfig("balthasar", cfg)
	if len(globalUser.Model.Fallbacks) != 1 {
		t.Fatalf("global fallback count = %d, want 1: %+v", len(globalUser.Model.Fallbacks), globalUser.Model.Fallbacks)
	}
	globalFallback := globalUser.Model.Fallbacks[0]
	if globalFallback.APIKey != "fallback-key" || globalFallback.BaseURL != "https://fallback.example.com" || globalFallback.ModelName != "fallback-model" {
		t.Fatalf("global fallback wrong: %+v", globalFallback)
	}

	role := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	if role.Model.BaseURL != "https://role.example.com" || role.Model.ModelName != "role-model" {
		t.Fatalf("role primary wrong: %+v", role.Model)
	}
	if len(role.Model.Fallbacks) != 1 {
		t.Fatalf("role fallback count = %d, want 1: %+v", len(role.Model.Fallbacks), role.Model.Fallbacks)
	}
	roleFallback := role.Model.Fallbacks[0]
	if roleFallback.APIKey != "global-key" || roleFallback.BaseURL != "https://role-fallback.example.com" || roleFallback.ModelName != "role-fallback-model" {
		t.Fatalf("role fallback should inherit the role primary key and override URL/model: %+v", roleFallback)
	}
	if len(roleFallback.Fallbacks) != 0 {
		t.Fatalf("provider must not recursively embed fallbacks: %+v", roleFallback.Fallbacks)
	}

	if got := cfg.CommanderModelRef(); len(got.Fallbacks) != 1 || got.Fallbacks[0].ModelName != "fallback-model" {
		t.Fatalf("commander should inherit global providers: %+v", got)
	}
}

func TestConfigValidate_ModelProviders(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	cfg.Model.Providers = []bootstrap.ModelSpec{{APIKey: "partial"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "model.providers[0]") {
		t.Fatalf("expected global provider validation error, got %v", err)
	}
	cfg.Model.Providers = []bootstrap.ModelSpec{{ModelName: "nested", Providers: []bootstrap.ModelSpec{{ModelName: "nested-child"}}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "nested providers are not supported") {
		t.Fatalf("expected nested provider validation error, got %v", err)
	}
	cfg.Model.Providers = nil
	cfg.Magi.Melchior.Model = &bootstrap.ModelSpec{Providers: []bootstrap.ModelSpec{{}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "magi.melchior.model.providers[0]") {
		t.Fatalf("expected role provider validation error, got %v", err)
	}
}

func TestConfigValidate_PerRoleModelOverride(t *testing.T) {
	cfg := &bootstrap.Config{}
	cfg.Model.APIKey = "k"
	cfg.Model.ModelName = "m"
	cfg.Magi.MaxDebateRounds = 1
	cfg.Magi.MaxSteps = 1
	cfg.Magi.TimeoutSeconds = 1
	cfg.Magi.CallTimeoutSeconds = 1

	// api_key without model_name on a role override must fail fast.
	cfg.Magi.Melchior.Model = &bootstrap.ModelSpec{APIKey: "role-key"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "melchior model") {
		t.Fatalf("expected melchior model validation error, got %v", err)
	}
	cfg.Magi.Melchior.Model = nil

	// judge override api_key without model_name must fail fast.
	cfg.Judge.Model = &bootstrap.ModelSpec{APIKey: "judge-key"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "judge model") {
		t.Fatalf("expected judge model validation error, got %v", err)
	}
	cfg.Judge.Model = nil

	// an empty override is a no-op and must pass.
	cfg.Judge.Model = &bootstrap.ModelSpec{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty judge override should pass, got %v", err)
	}
}
