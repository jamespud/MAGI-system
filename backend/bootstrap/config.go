package bootstrap

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Config is the root YAML configuration for MAGI server.
type Config struct {
	Model struct {
		APIKey             string  `yaml:"api_key"`
		BaseURL            string  `yaml:"base_url"`
		ModelName          string  `yaml:"model_name"`
		ModelID            int64   `yaml:"model_id"`
		PricePerMInputUSD  float64 `yaml:"price_per_m_input_usd"`
		PricePerMOutputUSD float64 `yaml:"price_per_m_output_usd"`
	} `yaml:"model"`
	Magi struct {
		MaxDebateRounds  int      `yaml:"max_debate_rounds"`
		MaxSteps         int      `yaml:"max_steps"`
		TimeoutSeconds   int      `yaml:"timeout_seconds"`
		CallTimeoutSeconds int    `yaml:"call_timeout_seconds"`
		Melchior         MagiSpec `yaml:"melchior"`
		Balthasar       MagiSpec `yaml:"balthasar"`
		Casper          MagiSpec `yaml:"casper"`
	} `yaml:"magi"`
	Database struct {
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"`
	} `yaml:"database"`
	Tavily struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"tavily"`
	Auth struct {
		Enabled bool         `yaml:"enabled"`
		APIKeys []APIKeySpec `yaml:"api_keys"`
	} `yaml:"auth"`
	Limits struct {
		MaxConcurrentRunsPerUser int `yaml:"max_concurrent_runs_per_user"`
	} `yaml:"limits"`
	CodeRunner struct {
		Enabled          *bool    `yaml:"enabled"`
		TimeoutSeconds   int      `yaml:"timeout_seconds"`
		MaxCodeChars     int      `yaml:"max_code_chars"`
		AllowedLanguages []string `yaml:"allowed_languages"`
		BlockedPatterns  []string `yaml:"blocked_patterns"`
	} `yaml:"code_runner"`
	ToolPolicy struct {
		RequireApproval []string `yaml:"require_approval"`
		AutoApproved    []string `yaml:"auto_approved"`
	} `yaml:"tool_policy"`
	Tracing struct {
		Enabled     bool   `yaml:"enabled"`
		ServiceName string `yaml:"service_name"`
	} `yaml:"tracing"`
	Embedding     EmbeddingConfig `yaml:"embedding"`
	Milvus        MilvusConfig    `yaml:"milvus"`
	Elasticsearch ESConfig        `yaml:"elasticsearch"`
	RAG           RAGConfig       `yaml:"rag"`
}

type APIKeySpec struct {
	Name   string `yaml:"name"`
	Key    string `yaml:"key"`
	UserID int64  `yaml:"user_id"`
	Role   string `yaml:"role"`
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

type MagiSpec struct {
	Persona          string               `yaml:"persona"`
	PersonaDef       PersonaDefSpec       `yaml:"persona_def"`
	Dimensions       []DimensionSpec      `yaml:"dimensions"`
	RiskTendency     string               `yaml:"risk_tendency"`
	RiskPolicy       RiskPolicySpec       `yaml:"risk_policy"`
	RolePolicy       RolePolicySpec       `yaml:"role_policy"`
	Evidence         EvidenceSpec         `yaml:"evidence"`
	ReflectionPolicy ReflectionPolicySpec `yaml:"reflection_policy"`
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
	if len(cfg.ToolPolicy.RequireApproval) == 0 {
		cfg.ToolPolicy.RequireApproval = []string{"code_runner"}
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
		Model: entity.ModelRef{
			APIKey:             cfg.Model.APIKey,
			BaseURL:            cfg.Model.BaseURL,
			ModelName:          cfg.Model.ModelName,
			ModelID:            cfg.Model.ModelID,
			PricePerMInputUSD:  cfg.Model.PricePerMInputUSD,
			PricePerMOutputUSD: cfg.Model.PricePerMOutputUSD,
		},
		ReflectionPolicy: entity.ReflectionPolicy{
			RequireJustification: s.ReflectionPolicy.RequireJustification,
			RequireNewEvidence:   s.ReflectionPolicy.RequireNewEvidence,
		},
		Tools: []entity.ToolBinding{
			{Source: entity.ToolSourceLocal, ToolName: "web_search"},
		},
		LoopPolicy: entity.LoopPolicy{
			MaxSteps:                         cfg.Magi.MaxSteps,
			Timeout:                          time.Duration(cfg.Magi.TimeoutSeconds) * time.Second,
			CallTimeout:                      time.Duration(cfg.Magi.CallTimeoutSeconds) * time.Second,
			MaxGateFailures:                  3,
			MaxConsecutiveToolFailures:       5,
			MaxConsecutiveValidationFailures: 5,
			TokenBudget:                      150000,
			MaxToolCalls:                     5,
		},
	}
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
	if c.Auth.Enabled {
		if len(c.Auth.APIKeys) == 0 {
			return fmt.Errorf("auth: at least one api_key is required when enabled")
		}
		for _, k := range c.Auth.APIKeys {
			if k.Key == "" || k.UserID <= 0 || k.Role == "" {
				return fmt.Errorf("auth: each api_key needs non-empty key, user_id > 0 and role")
			}
		}
	}
	if c.Limits.MaxConcurrentRunsPerUser < 0 {
		return fmt.Errorf("limits: max_concurrent_runs_per_user cannot be negative")
	}
	if c.Magi.MaxDebateRounds < 1 || c.Magi.MaxSteps < 1 || c.Magi.TimeoutSeconds < 1 || c.Magi.CallTimeoutSeconds < 1 {
		return fmt.Errorf("magi: max_debate_rounds, max_steps, timeout_seconds and call_timeout_seconds must be positive")
	}
	return nil
}
