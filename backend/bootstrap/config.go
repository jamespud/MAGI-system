package bootstrap

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

// Config is the root YAML configuration for MAGI server.
type Config struct {
	Model ModelSpec `yaml:"model"`
	Magi  struct {
		MaxDebateRounds        int      `yaml:"max_debate_rounds"`
		MaxSteps               int      `yaml:"max_steps"`
		TimeoutSeconds         int      `yaml:"timeout_seconds"`
		CallTimeoutSeconds     int      `yaml:"call_timeout_seconds"`
		ApprovalTimeoutSeconds int      `yaml:"approval_timeout_seconds"`
		TokenBudget            int      `yaml:"token_budget"`
		CompactionThreshold    float64  `yaml:"compaction_threshold"`
		Melchior               MagiSpec `yaml:"melchior"`
		Balthasar              MagiSpec `yaml:"balthasar"`
		Casper                 MagiSpec `yaml:"casper"`
	} `yaml:"magi"`
	Database struct {
		Driver          string `yaml:"driver"`
		DSN             string `yaml:"dsn"`
		LogLevel        string `yaml:"log_level"`         // silent|error|warn|info (default warn)
		SlowThresholdMs int    `yaml:"slow_threshold_ms"` // GORM slow-query threshold (default 200ms)
	} `yaml:"database"`
	Tavily struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"tavily"`
	Search SearchConfig `yaml:"search"`
	Auth   struct {
		Enabled bool         `yaml:"enabled"`
		APIKeys []APIKeySpec `yaml:"api_keys"`
	} `yaml:"auth"`
	Limits struct {
		MaxConcurrentRunsPerUser int     `yaml:"max_concurrent_runs_per_user"`
		MaxTokensPerUser         int64   `yaml:"max_tokens_per_user"`
		MaxCostUSDPerUser        float64 `yaml:"max_cost_usd_per_user"`
	} `yaml:"limits"`
	CodeRunner struct {
		Enabled          *bool                  `yaml:"enabled"`
		TimeoutSeconds   int                    `yaml:"timeout_seconds"`
		MaxCodeChars     int                    `yaml:"max_code_chars"`
		AllowedLanguages []string               `yaml:"allowed_languages"`
		BlockedPatterns  []string               `yaml:"blocked_patterns"`
		AllowEnv         []string               `yaml:"allow_env"`
		AllowRead        []string               `yaml:"allow_read"`
		AllowWrite       []string               `yaml:"allow_write"`
		AllowNet         []string               `yaml:"allow_net"`
		AllowRun         []string               `yaml:"allow_run"`
		AllowFFI         []string               `yaml:"allow_ffi"`
		NodeModulesDir   string                 `yaml:"node_modules_dir"`
		MemoryLimitMB    int64                  `yaml:"memory_limit_mb"`
		Docker           DockerCodeRunnerConfig `yaml:"docker"`
	} `yaml:"code_runner"`
	DBTool       DBToolConfig       `yaml:"db_tool"`
	FeedbackTool FeedbackToolConfig `yaml:"feedback_tool"`
	FileTool     FileToolConfig     `yaml:"file_tool"`
	RepoTool     RepoToolConfig     `yaml:"repo_tool"`
	WebTool      WebToolConfig      `yaml:"web_tool"`
	DelegateTool DelegateToolConfig `yaml:"delegate_tool"`
	ToolPolicy   struct {
		RequireApproval []string `yaml:"require_approval"`
		AutoApproved    []string `yaml:"auto_approved"`
	} `yaml:"tool_policy"`
	MCP struct {
		Servers []MCPServerConfig `yaml:"servers"`
	} `yaml:"mcp"`
	Tracing struct {
		Enabled      bool   `yaml:"enabled"`
		ServiceName  string `yaml:"service_name"`
		OTLPEndpoint string `yaml:"otlp_endpoint"`
	} `yaml:"tracing"`
	HTTPRateLimit struct {
		Enabled          bool `yaml:"enabled"`
		PerUserPerMinute int  `yaml:"per_user_per_minute"`
		PerIPPerMinute   int  `yaml:"per_ip_per_minute"`
	} `yaml:"http_rate_limit"`
	Metrics struct {
		AuthRequired bool `yaml:"auth_required"` // /metrics requires an admin role when true
	} `yaml:"metrics"`
	Embedding     EmbeddingConfig `yaml:"embedding"`
	Milvus        MilvusConfig    `yaml:"milvus"`
	Elasticsearch ESConfig        `yaml:"elasticsearch"`
	RAG           RAGConfig       `yaml:"rag"`
	Benchmark     BenchmarkConfig `yaml:"benchmark"`
	ToolQuota     ToolQuotaConfig `yaml:"tool_quota"`
	Commander     CommanderSpec   `yaml:"commander"`
	Judge         JudgeSpec       `yaml:"judge"`
}

// DockerCodeRunnerConfig enables the optional Docker sandbox runtime as an
// alternative to the Coze WASM sandbox.
type DockerCodeRunnerConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Image          string `yaml:"image"`
	MemoryMB       int64  `yaml:"memory_mb"`
	CPUs           string `yaml:"cpus"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type ToolQuotaConfig struct {
	DefaultPerMinute int            `yaml:"default_per_minute"`
	Tools            map[string]int `yaml:"tools"`
}

type BenchmarkConfig struct {
	RunsPerItem             int     `yaml:"runs_per_item"`
	RegressionThreshold     float64 `yaml:"regression_threshold"`
	AutoIntervalSeconds     int     `yaml:"auto_interval_seconds"`
	AutoRunsPerItem         int     `yaml:"auto_runs_per_item"`
	AutoRegressionThreshold float64 `yaml:"auto_regression_threshold"`
}

type APIKeySpec struct {
	Name    string `yaml:"name"`
	Key     string `yaml:"key"`
	KeyHash string `yaml:"key_hash"`
	UserID  int64  `yaml:"user_id"`
	Role    string `yaml:"role"`
}

// MCPServerConfig describes one external MCP server. stdio servers are child
// processes spawned by MAGI; http servers use the MCP Streamable HTTP
// transport.
type MCPServerConfig struct {
	Name           string            `yaml:"name"`
	Transport      string            `yaml:"transport"` // "stdio" | "http"
	Command        string            `yaml:"command"`   // stdio: executable to spawn
	Args           []string          `yaml:"args"`
	URL            string            `yaml:"url"` // http: base URL of the MCP endpoint
	Env            map[string]string `yaml:"env"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Headers        map[string]string `yaml:"headers"`
	RetryAttempts  int               `yaml:"retry_attempts"`
}

type SearchConfig struct {
	Providers []SearchProviderConfig `yaml:"providers"`
}

type SearchProviderConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"`
}

// DBToolConfig configures the built-in read-only database query tool.
type DBToolConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Driver          string   `yaml:"driver"`
	DSN             string   `yaml:"dsn"`
	MaxRows         int      `yaml:"max_rows"`
	MaxQueryChars   int      `yaml:"max_query_chars"`
	TimeoutSeconds  int      `yaml:"timeout_seconds"`
	BlockedPrefixes []string `yaml:"blocked_prefixes"`
}

// FeedbackToolConfig controls the built-in deterministic check_output tool.
type FeedbackToolConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// FileToolConfig configures the built-in read-only file query tool.
type FileToolConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Roots        []string `yaml:"roots"`
	MaxFileBytes int64    `yaml:"max_file_bytes"`
	MaxListItems int      `yaml:"max_list_items"`
}

// RepoToolConfig configures the built-in read-only repository query tool.
type RepoToolConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Roots        []string `yaml:"roots"`
	Includes     []string `yaml:"includes"`
	MaxResults   int      `yaml:"max_results"`
	MaxFileBytes int64    `yaml:"max_file_bytes"`
}

// WebToolConfig configures the built-in restricted URL fetch tool.
type WebToolConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedDomains []string `yaml:"allowed_domains"`
	MaxBytes       int64    `yaml:"max_bytes"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
}

// DelegateToolConfig enables the dynamic sub-investigation delegation tool.
type DelegateToolConfig struct {
	Enabled bool `yaml:"enabled"`
}

type EmbeddingConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	ModelName string `yaml:"model_name"`
	Dim       int    `yaml:"dim"`
}

type MilvusConfig struct {
	Address    string `yaml:"address"`
	Collection string `yaml:"collection"`
}

type ESConfig struct {
	Addresses []string `yaml:"addresses"`
	Index     string   `yaml:"index"`
}

type RAGConfig struct {
	Levels             []int  `yaml:"levels"`
	TopK               int    `yaml:"top_k"`
	RRFK               int    `yaml:"rrf_k"`
	MergeThreshold900  int    `yaml:"merge_threshold_900"`
	MergeThreshold1800 int    `yaml:"merge_threshold_1800"`
	OrphanStrategy     string `yaml:"orphan_strategy"`
	StoreAsync         bool   `yaml:"store_async"`
	StoreWorkers       int    `yaml:"store_workers"`
}

type ModelSpec struct {
	APIKey             string      `yaml:"api_key"`
	BaseURL            string      `yaml:"base_url"`
	ModelName          string      `yaml:"model_name"`
	ModelID            int64       `yaml:"model_id"`
	PricePerMInputUSD  float64     `yaml:"price_per_m_input_usd"`
	PricePerMOutputUSD float64     `yaml:"price_per_m_output_usd"`
	Providers          []ModelSpec `yaml:"providers"`
}

// empty reports whether an override is entirely unset. An empty override is a
// no-op that falls back to the global model; it is not a validation error.
func (m *ModelSpec) empty() bool {
	return m == nil ||
		(m.APIKey == "" && m.BaseURL == "" && m.ModelName == "" && m.ModelID == 0 &&
			m.PricePerMInputUSD == 0 && m.PricePerMOutputUSD == 0 && len(m.Providers) == 0)
}

type CommanderSpec struct {
	Model *ModelSpec `yaml:"model"`
}

type JudgeSpec struct {
	Model *ModelSpec `yaml:"model"`
}

type MagiSpec struct {
	Persona          string               `yaml:"persona"`
	PersonaDef       PersonaDefSpec       `yaml:"persona_def"`
	Dimensions       []DimensionSpec      `yaml:"dimensions"`
	RiskTendency     string               `yaml:"risk_tendency"`
	RiskPolicy       RiskPolicySpec       `yaml:"risk_policy"`
	RolePolicy       RolePolicySpec       `yaml:"role_policy"`
	Evidence         EvidenceSpec         `yaml:"evidence"`
	ReflectionPolicy ReflectionPolicySpec `yaml:"reflection_policy"`
	Model            *ModelSpec           `yaml:"model"`
}

type PersonaDefSpec struct {
	SystemPrompt string `yaml:"system_prompt"`
	Voice        string `yaml:"voice"`
}

type RiskPolicySpec struct {
	MaxAcceptableRisk float64 `yaml:"max_acceptable_risk"`
}

type RolePolicySpec struct {
	EnforceAssessment       *bool    `yaml:"enforce_assessment"`
	RequiredAssessment      string   `yaml:"required_assessment"`
	MaxResidualRisk         *float64 `yaml:"max_residual_risk"`
	MinTechnicalScore       *float64 `yaml:"min_technical_score"`
	MinOpportunityScore     *float64 `yaml:"min_opportunity_score"`
	MinWeightedUtilityScore *float64 `yaml:"min_weighted_utility_score"`
	DebateDirective         string   `yaml:"debate_directive"`
}

type ReflectionPolicySpec struct {
	RequireJustification bool `yaml:"require_justification"`
	RequireNewEvidence   bool `yaml:"require_new_evidence"`
}

type DimensionSpec struct {
	Code        string  `yaml:"code"`
	Description string  `yaml:"description"`
	Weight      float64 `yaml:"weight"`
}

type EvidenceSpec struct {
	MinEvidenceCount     int           `yaml:"min_evidence_count"`
	MinQuantitativeCount int           `yaml:"min_quantitative_count"`
	MinReliability       float64       `yaml:"min_reliability"`
	RequireOwnCollected  bool          `yaml:"require_own_collected"`
	RequiredClaimCount   int           `yaml:"required_claim_count"`
	RequiredTypes        []TypeReqSpec `yaml:"required_types"`
	CustomRules          []RuleSpec    `yaml:"custom_rules"`
}

type TypeReqSpec struct {
	Type     string `yaml:"type"`
	MinCount int    `yaml:"min_count"`
}

type RuleSpec struct {
	Code string `yaml:"code"`
}

// LoadConfig reads and parses the YAML config file, applying defaults.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Magi.MaxDebateRounds == 0 {
		cfg.Magi.MaxDebateRounds = 2
	}
	if cfg.Magi.MaxSteps == 0 {
		cfg.Magi.MaxSteps = 12
	}
	if cfg.Magi.TimeoutSeconds == 0 {
		cfg.Magi.TimeoutSeconds = 600
	}
	if cfg.Magi.CallTimeoutSeconds == 0 {
		cfg.Magi.CallTimeoutSeconds = 180
	}
	if cfg.FeedbackTool.Enabled == nil {
		enabled := true
		cfg.FeedbackTool.Enabled = &enabled
	}
	if cfg.Magi.ApprovalTimeoutSeconds == 0 {
		cfg.Magi.ApprovalTimeoutSeconds = 3600
	}
	if cfg.Magi.TokenBudget == 0 {
		cfg.Magi.TokenBudget = 150000
	}
	// Compaction is disabled by default (0 = no limit / never compact).
	// The 132k-token compaction window proved too small for long decisions:
	// summaries degrade EV-ID fidelity and can cause repeated gate failures.
	// Set compaction_threshold explicitly (e.g. 0.7) to re-enable.
	if len(cfg.ToolPolicy.RequireApproval) == 0 {
		cfg.ToolPolicy.RequireApproval = []string{"code_runner"}
	}
	if cfg.CodeRunner.MemoryLimitMB == 0 {
		cfg.CodeRunner.MemoryLimitMB = 100
	}
	for i := range cfg.MCP.Servers {
		if cfg.MCP.Servers[i].TimeoutSeconds == 0 {
			cfg.MCP.Servers[i].TimeoutSeconds = 60
		}
	}
	if cfg.Model.PricePerMInputUSD == 0 {
		cfg.Model.PricePerMInputUSD = 2.5
	}
	if cfg.Model.PricePerMOutputUSD == 0 {
		cfg.Model.PricePerMOutputUSD = 10
	}

	if cfg.RAG.TopK == 0 {
		cfg.RAG.TopK = 15
	}
	if cfg.RAG.RRFK == 0 {
		cfg.RAG.RRFK = 60
	}
	if cfg.RAG.MergeThreshold900 == 0 {
		cfg.RAG.MergeThreshold900 = 3
	}
	if cfg.RAG.MergeThreshold1800 == 0 {
		cfg.RAG.MergeThreshold1800 = 2
	}
	if cfg.RAG.OrphanStrategy == "" {
		cfg.RAG.OrphanStrategy = "keep_300"
	}
	if cfg.RAG.StoreWorkers == 0 {
		cfg.RAG.StoreWorkers = 4
	}
	if cfg.Benchmark.RunsPerItem == 0 {
		cfg.Benchmark.RunsPerItem = 1
	}
	if len(cfg.RAG.Levels) == 0 {
		cfg.RAG.Levels = []int{1800, 900, 300}
	}
	applyEnvOverrides(&cfg)
	return &cfg, nil
}

// applyEnvOverrides overrides config fields from environment variables when set.
// The containerized deployment injects DSN and secrets this way (12-factor)
// instead of baking them into the image. Empty vars leave the YAML value intact,
// so local `make debug` (which sets none of these) behaves unchanged.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MAGI_DB_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("MAGI_DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("MAGI_DB_LOG_LEVEL"); v != "" {
		cfg.Database.LogLevel = v
	}
	if v := os.Getenv("MAGI_DB_SLOW_THRESHOLD_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Database.SlowThresholdMs = n
		}
	}
	if v := os.Getenv("MAGI_MODEL_API_KEY"); v != "" {
		cfg.Model.APIKey = v
	}
	if v := os.Getenv("MAGI_MODEL_BASE_URL"); v != "" {
		cfg.Model.BaseURL = v
	}
	if v := os.Getenv("MAGI_MODEL_NAME"); v != "" {
		cfg.Model.ModelName = v
	}
	if v := os.Getenv("MAGI_TAVILY_API_KEY"); v != "" {
		cfg.Tavily.APIKey = v
	}
	if v := os.Getenv("MAGI_AUTH_ENABLED"); v != "" {
		cfg.Auth.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("MAGI_AUTH_API_KEYS"); v != "" {
		cfg.Auth.APIKeys = parseAPIKeys(v)
	}
	if v := os.Getenv("MAGI_EMBEDDING_API_KEY"); v != "" {
		cfg.Embedding.APIKey = v
	}
	if v := os.Getenv("MAGI_EMBEDDING_BASE_URL"); v != "" {
		cfg.Embedding.BaseURL = v
	}
	if v := os.Getenv("MAGI_EMBEDDING_MODEL_NAME"); v != "" {
		cfg.Embedding.ModelName = v
	}
	if v := os.Getenv("MAGI_MILVUS_ADDRESS"); v != "" {
		cfg.Milvus.Address = v
	}
	if v := os.Getenv("MAGI_ES_ADDRESSES"); v != "" {
		cfg.Elasticsearch.Addresses = []string{v}
	}
}

// parseAPIKeys parses MAGI_AUTH_API_KEYS entries separated by ';', each in the
// form userID:role:name:key (the key may contain colons).
func parseAPIKeys(raw string) []APIKeySpec {
	var out []APIKeySpec
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 4)
		if len(parts) != 4 {
			continue
		}
		uid, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, APIKeySpec{
			UserID: uid,
			Role:   strings.TrimSpace(parts[1]),
			Name:   strings.TrimSpace(parts[2]),
			Key:    strings.TrimSpace(parts[3]),
		})
	}
	return out
}

// modelRef builds the default ModelRef from the global model config.
func (c *Config) modelRef() entity.ModelRef {
	ref := modelSpecToRef(c.Model)
	ref.Fallbacks = providerRefs(ref, c.Model.Providers)
	return ref
}

func modelSpecToRef(spec ModelSpec) entity.ModelRef {
	return entity.ModelRef{
		APIKey:             spec.APIKey,
		BaseURL:            spec.BaseURL,
		ModelName:          spec.ModelName,
		ModelID:            spec.ModelID,
		PricePerMInputUSD:  spec.PricePerMInputUSD,
		PricePerMOutputUSD: spec.PricePerMOutputUSD,
	}
}

func providerRefs(fallback entity.ModelRef, providers []ModelSpec) []entity.ModelRef {
	refs := make([]entity.ModelRef, 0, len(providers))
	for _, provider := range providers {
		override := provider
		override.Providers = nil
		ref := applyModelSpec(fallback, &override)
		ref.Fallbacks = nil
		if modelRefsEqual(fallback, ref) {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func applyModelSpec(fallback entity.ModelRef, override *ModelSpec) entity.ModelRef {
	ref := fallback
	if override == nil {
		return ref
	}
	if override.APIKey != "" {
		ref.APIKey = override.APIKey
	}
	if override.BaseURL != "" {
		ref.BaseURL = override.BaseURL
	}
	if override.ModelName != "" {
		ref.ModelName = override.ModelName
	}
	if override.ModelID != 0 {
		ref.ModelID = override.ModelID
	}
	if override.PricePerMInputUSD != 0 {
		ref.PricePerMInputUSD = override.PricePerMInputUSD
	}
	if override.PricePerMOutputUSD != 0 {
		ref.PricePerMOutputUSD = override.PricePerMOutputUSD
	}
	return ref
}

func modelRefsEqual(a, b entity.ModelRef) bool {
	return a.APIKey == b.APIKey && a.BaseURL == b.BaseURL && a.ModelName == b.ModelName &&
		a.ModelID == b.ModelID && a.PricePerMInputUSD == b.PricePerMInputUSD &&
		a.PricePerMOutputUSD == b.PricePerMOutputUSD
}

// resolveModelRef returns the fallback ModelRef overlaid with any fields set
// on the override. Zero values on the override fall back to the global model,
// so a per-role override may specify only api_key/base_url/model_name (or a
// model_id in Coze mode) and inherit the remaining fields.
func resolveModelRef(fallback entity.ModelRef, override *ModelSpec) entity.ModelRef {
	ref := applyModelSpec(fallback, override)
	if override == nil || override.empty() {
		return ref
	}
	providers := fallback.Fallbacks
	if override != nil && len(override.Providers) > 0 {
		providers = providerRefs(ref, override.Providers)
	}
	ref.Fallbacks = dedupeModelRefs(append([]entity.ModelRef{ref}, providers...))[1:]
	return ref
}

func dedupeModelRefs(refs []entity.ModelRef) []entity.ModelRef {
	out := make([]entity.ModelRef, 0, len(refs))
	for _, ref := range refs {
		duplicate := false
		for _, existing := range out {
			if modelRefsEqual(ref, existing) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, ref)
		}
	}
	return out
}

// CommanderModelRef returns the commander's resolved model ref: the
// commander.model override if set, otherwise the global model.
func (c *Config) CommanderModelRef() entity.ModelRef {
	return resolveModelRef(c.modelRef(), c.Commander.Model)
}

// JudgeModelRef returns the judge's resolved model ref: the judge.model
// override if set, otherwise the global model.
func (c *Config) JudgeModelRef() entity.ModelRef {
	return resolveModelRef(c.modelRef(), c.Judge.Model)
}

// webSearchProviderSpecs resolves explicit search providers and retains the
// legacy tavily.api_key setting as the primary provider when both are present.
func webSearchProviderSpecs(cfg *Config) []SearchProviderConfig {
	providers := make([]SearchProviderConfig, 0, len(cfg.Search.Providers)+1)
	for _, provider := range cfg.Search.Providers {
		if strings.TrimSpace(provider.APIKey) == "" {
			continue
		}
		providers = append(providers, provider)
	}
	if cfg.Tavily.APIKey == "" {
		return providers
	}
	hasTavily := false
	for _, provider := range providers {
		if strings.EqualFold(provider.Provider, magi.SearchProviderTavily) {
			hasTavily = true
			break
		}
	}
	if !hasTavily {
		providers = append([]SearchProviderConfig{{
			Provider: magi.SearchProviderTavily,
			APIKey:   cfg.Tavily.APIKey,
		}}, providers...)
	}
	return providers
}

// enabledLocalTools returns the built-in local tools that should be exposed
// to agents based on configuration.
func enabledLocalTools(cfg *Config) []string {
	tools := make([]string, 0, 4)
	if len(webSearchProviderSpecs(cfg)) > 0 {
		tools = append(tools, "web_search")
	}
	if cfg.DBTool.Enabled {
		tools = append(tools, magi.DBQueryToolName)
	}
	if feedbackToolEnabled(cfg) {
		tools = append(tools, magi.FeedbackToolName)
	}
	if cfg.FileTool.Enabled {
		tools = append(tools, magi.FileToolName)
	}
	if cfg.RepoTool.Enabled {
		tools = append(tools, magi.RepoToolName)
	}
	if cfg.WebTool.Enabled {
		tools = append(tools, magi.WebFetchToolName)
	}
	if cfg.DelegateTool.Enabled {
		tools = append(tools, magi.DelegateToolName)
	}
	return tools
}

func feedbackToolEnabled(cfg *Config) bool {
	return cfg != nil && cfg.FeedbackTool.Enabled != nil && *cfg.FeedbackTool.Enabled
}

func validateSearchProviders(providers []SearchProviderConfig) error {
	seen := make(map[string]bool, len(providers))
	for i := range providers {
		provider := strings.ToLower(strings.TrimSpace(providers[i].Provider))
		if provider == "" && strings.TrimSpace(providers[i].APIKey) == "" {
			continue
		}
		if provider == "" {
			return fmt.Errorf("search.providers[%d]: provider is required", i)
		}
		if provider != magi.SearchProviderTavily && provider != magi.SearchProviderBrave {
			return fmt.Errorf("search.providers[%d]: unsupported provider %q (use tavily or brave)", i, provider)
		}
		if strings.TrimSpace(providers[i].APIKey) == "" {
			// Explicit keyless entries are disabled placeholders, matching the
			// shipped example and legacy tavily.api_key behavior.
			continue
		}
		if seen[provider] {
			return fmt.Errorf("search.providers[%d]: duplicate provider %q", i, provider)
		}
		seen[provider] = true
	}
	return nil
}

func validateDBTool(tool *DBToolConfig, defaultDriver, defaultDSN string) error {
	if tool == nil {
		return nil
	}
	driver := strings.TrimSpace(tool.Driver)
	if driver == "" {
		driver = strings.TrimSpace(defaultDriver)
	}
	if driver != "mysql" && driver != "sqlite3" {
		return fmt.Errorf("db_tool: driver must be \"mysql\" or \"sqlite3\" when enabled")
	}
	dsn := strings.TrimSpace(tool.DSN)
	if dsn == "" {
		dsn = strings.TrimSpace(defaultDSN)
	}
	if dsn == "" {
		return fmt.Errorf("db_tool: dsn is required when enabled (set db_tool.dsn or database.dsn)")
	}
	if tool.MaxRows < 0 || tool.MaxQueryChars < 0 || tool.TimeoutSeconds < 0 {
		return fmt.Errorf("db_tool: max_rows, max_query_chars and timeout_seconds cannot be negative")
	}
	return nil
}

// validateModelOverride returns an error for an invalid non-empty model
// override. Empty overrides fall back to the global model and are valid.
func validateModelProviders(scope string, providers []ModelSpec) error {
	for i := range providers {
		provider := providers[i]
		childScope := fmt.Sprintf("%s.providers[%d]", scope, i)
		if provider.empty() {
			return fmt.Errorf("%s must set at least one field", childScope)
		}
		if len(provider.Providers) > 0 {
			return fmt.Errorf("%s: nested providers are not supported", childScope)
		}
		if err := validateModelOverride(childScope, &provider); err != nil {
			return err
		}
	}
	return nil
}

func validateModelOverride(scope string, m *ModelSpec) error {
	if m == nil || m.empty() {
		return nil
	}
	// The override inherits unset fields from the global model, so the only
	// invalid shape is a partial direct-mode override: api_key set without a
	// model_name (and no coze model_id to fall back on).
	if m.APIKey != "" && m.ModelName == "" && m.ModelID == 0 {
		return fmt.Errorf("%s model: model_name is required when api_key is set (or set model_id for coze mode)", scope)
	}
	return validateModelProviders(scope+".model", m.Providers)
}

// ToConfig converts a MagiSpec to an entity.MagiConfig.
func (s *MagiSpec) ToConfig(code string, cfg *Config) *entity.MagiConfig {
	dims := make([]entity.UtilityDimension, len(s.Dimensions))
	for i, d := range s.Dimensions {
		dims[i] = entity.UtilityDimension{Code: d.Code, Description: d.Description, Weight: d.Weight}
	}
	reqTypes := make([]entity.EvidenceTypeRequirement, len(s.Evidence.RequiredTypes))
	for i, t := range s.Evidence.RequiredTypes {
		reqTypes[i] = entity.EvidenceTypeRequirement{Type: t.Type, MinCount: t.MinCount}
	}
	customRules := make([]entity.EvidenceRule, len(s.Evidence.CustomRules))
	for i, r := range s.Evidence.CustomRules {
		customRules[i] = entity.EvidenceRule{Code: r.Code}
	}
	var personaDef *entity.PersonaDefinition
	if s.PersonaDef.SystemPrompt != "" || s.PersonaDef.Voice != "" {
		personaDef = &entity.PersonaDefinition{SystemPrompt: s.PersonaDef.SystemPrompt, Voice: s.PersonaDef.Voice}
	}
	rolePolicy := entity.DefaultRolePolicy(code)
	if s.RolePolicy.EnforceAssessment != nil {
		rolePolicy.EnforceAssessment = *s.RolePolicy.EnforceAssessment
	}
	if s.RolePolicy.RequiredAssessment != "" {
		rolePolicy.RequiredAssessment = s.RolePolicy.RequiredAssessment
	}
	if s.RolePolicy.MaxResidualRisk != nil {
		rolePolicy.MaxResidualRisk = *s.RolePolicy.MaxResidualRisk
	}
	if s.RolePolicy.MinTechnicalScore != nil {
		rolePolicy.MinTechnicalScore = *s.RolePolicy.MinTechnicalScore
	}
	if s.RolePolicy.MinOpportunityScore != nil {
		rolePolicy.MinOpportunityScore = *s.RolePolicy.MinOpportunityScore
	}
	if s.RolePolicy.MinWeightedUtilityScore != nil {
		rolePolicy.MinWeightedUtilityScore = *s.RolePolicy.MinWeightedUtilityScore
	}
	if s.RolePolicy.DebateDirective != "" {
		rolePolicy.DebateDirective = s.RolePolicy.DebateDirective
	}
	return &entity.MagiConfig{
		Code:         code,
		Persona:      s.Persona,
		PersonaDef:   personaDef,
		Objective:    entity.ObjectiveFunction{Dimensions: dims},
		RiskTendency: entity.RiskTendency(s.RiskTendency),
		RiskPolicy: entity.RiskPolicy{
			Tendency:          entity.RiskTendency(s.RiskTendency),
			MaxAcceptableRisk: s.RiskPolicy.MaxAcceptableRisk,
		},
		RolePolicy: rolePolicy,
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount:     s.Evidence.MinEvidenceCount,
			MinQuantitativeCount: s.Evidence.MinQuantitativeCount,
			MinReliability:       s.Evidence.MinReliability,
			RequireOwnCollected:  s.Evidence.RequireOwnCollected,
			RequiredClaimCount:   s.Evidence.RequiredClaimCount,
			RequiredTypes:        reqTypes,
			CustomRules:          customRules,
		},
		Model: resolveModelRef(cfg.modelRef(), s.Model),
		ReflectionPolicy: entity.ReflectionPolicy{
			RequireJustification: s.ReflectionPolicy.RequireJustification,
			RequireNewEvidence:   s.ReflectionPolicy.RequireNewEvidence,
		},
		Tools: s.bindTools(cfg),
		LoopPolicy: entity.LoopPolicy{
			MaxSteps:                         cfg.Magi.MaxSteps,
			Timeout:                          time.Duration(cfg.Magi.TimeoutSeconds) * time.Second,
			CallTimeout:                      time.Duration(cfg.Magi.CallTimeoutSeconds) * time.Second,
			MaxGateFailures:                  3,
			MaxConsecutiveToolFailures:       5,
			MaxConsecutiveValidationFailures: 5,
			TokenBudget:                      cfg.Magi.TokenBudget,
			MaxToolCalls:                     5,
			ApprovalTimeout:                  time.Duration(cfg.Magi.ApprovalTimeoutSeconds) * time.Second,
			TokenCompactionThreshold:         cfg.Magi.CompactionThreshold,
		},
	}
}

func (s *MagiSpec) bindTools(cfg *Config) []entity.ToolBinding {
	tools := []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	if cfg.DBTool.Enabled {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.DBQueryToolName})
	}
	if feedbackToolEnabled(cfg) {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.FeedbackToolName})
	}
	if cfg.FileTool.Enabled {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.FileToolName})
	}
	if cfg.RepoTool.Enabled {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.RepoToolName})
	}
	if cfg.WebTool.Enabled {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.WebFetchToolName})
	}
	if cfg.DelegateTool.Enabled {
		tools = append(tools, entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: magi.DelegateToolName})
	}
	return tools
}

// Validate returns a descriptive error for invalid or incomplete
// configurations, so the server fails fast instead of booting broken.
func (c *Config) Validate() error {
	if c.Model.APIKey == "" && c.Model.ModelID == 0 {
		return fmt.Errorf("model: set api_key+model_name (direct) or model_id (coze)")
	}
	if c.Model.APIKey != "" && c.Model.ModelName == "" {
		return fmt.Errorf("model: model_name is required when api_key is set")
	}
	if err := validateModelProviders("model", c.Model.Providers); err != nil {
		return err
	}
	if err := validateSearchProviders(c.Search.Providers); err != nil {
		return err
	}
	if c.DBTool.Enabled {
		if err := validateDBTool(&c.DBTool, c.Database.Driver, c.Database.DSN); err != nil {
			return err
		}
	}
	if c.Auth.Enabled {
		if len(c.Auth.APIKeys) == 0 {
			return fmt.Errorf("auth: at least one api_key is required when enabled")
		}
		for _, k := range c.Auth.APIKeys {
			if (k.Key == "" && k.KeyHash == "") || k.UserID <= 0 || k.Role == "" {
				return fmt.Errorf("auth: each api_key needs non-empty key, user_id > 0 and role")
			}
		}
	}
	if c.Limits.MaxConcurrentRunsPerUser < 0 {
		return fmt.Errorf("limits: max_concurrent_runs_per_user cannot be negative")
	}
	if c.Limits.MaxTokensPerUser < 0 {
		return fmt.Errorf("limits: max_tokens_per_user cannot be negative")
	}
	if c.Limits.MaxCostUSDPerUser < 0 {
		return fmt.Errorf("limits: max_cost_usd_per_user cannot be negative")
	}
	if c.CodeRunner.Docker.Enabled && strings.TrimSpace(c.CodeRunner.Docker.Image) == "" {
		return fmt.Errorf("code_runner.docker: image is required when enabled")
	}
	if c.FileTool.Enabled && len(c.FileTool.Roots) == 0 {
		return fmt.Errorf("file_tool: at least one root is required when enabled")
	}
	if c.RepoTool.Enabled && len(c.RepoTool.Roots) == 0 {
		return fmt.Errorf("repo_tool: at least one root is required when enabled")
	}
	if c.WebTool.Enabled && len(c.WebTool.AllowedDomains) == 0 {
		return fmt.Errorf("web_tool: at least one allowed domain is required when enabled")
	}
	if c.HTTPRateLimit.Enabled && c.HTTPRateLimit.PerUserPerMinute <= 0 && c.HTTPRateLimit.PerIPPerMinute <= 0 {
		return fmt.Errorf("http_rate_limit: at least one of per_user_per_minute / per_ip_per_minute must be positive when enabled")
	}
	if c.Magi.MaxDebateRounds < 1 || c.Magi.MaxSteps < 1 || c.Magi.TimeoutSeconds < 1 || c.Magi.CallTimeoutSeconds < 1 {
		return fmt.Errorf("magi: max_debate_rounds, max_steps, timeout_seconds and call_timeout_seconds must be positive")
	}
	for name, spec := range map[string]MagiSpec{"melchior": c.Magi.Melchior, "balthasar": c.Magi.Balthasar, "casper": c.Magi.Casper} {
		if err := validateModelOverride("magi."+name, spec.Model); err != nil {
			return err
		}
	}
	if err := validateModelOverride("commander", c.Commander.Model); err != nil {
		return err
	}
	if err := validateModelOverride("judge", c.Judge.Model); err != nil {
		return err
	}
	seenMCP := make(map[string]bool, len(c.MCP.Servers))
	for i, s := range c.MCP.Servers {
		if s.Name == "" {
			return fmt.Errorf("mcp: server #%d: name is required", i)
		}
		if seenMCP[s.Name] {
			return fmt.Errorf("mcp: duplicate server name %q", s.Name)
		}
		seenMCP[s.Name] = true
		switch s.Transport {
		case "stdio":
			if s.Command == "" {
				return fmt.Errorf("mcp: server %q: stdio transport requires command", s.Name)
			}
		case "http":
			if s.URL == "" {
				return fmt.Errorf("mcp: server %q: http transport requires url", s.Name)
			}
			if s.RetryAttempts < 0 {
				return fmt.Errorf("mcp: server %q: retry_attempts cannot be negative", s.Name)
			}
		default:
			return fmt.Errorf("mcp: server %q: transport must be \"stdio\" or \"http\"", s.Name)
		}
	}
	return nil
}
