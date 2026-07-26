package bootstrap

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Config is the root YAML configuration for MAGI server.
type Config struct {
	Model struct {
		APIKey    string `yaml:"api_key"`
		BaseURL   string `yaml:"base_url"`
		ModelName string `yaml:"model_name"`
	} `yaml:"model"`
	Magi struct {
		MaxDebateRounds int      `yaml:"max_debate_rounds"`
		MaxSteps        int      `yaml:"max_steps"`
		TimeoutSeconds  int      `yaml:"timeout_seconds"`
		Melchior        MagiSpec `yaml:"melchior"`
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
	Embedding     EmbeddingConfig `yaml:"embedding"`
	Milvus        MilvusConfig    `yaml:"milvus"`
	Elasticsearch ESConfig        `yaml:"elasticsearch"`
	RAG           RAGConfig       `yaml:"rag"`
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
		cfg.Magi.TimeoutSeconds = 120
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
			APIKey:    cfg.Model.APIKey,
			BaseURL:   cfg.Model.BaseURL,
			ModelName: cfg.Model.ModelName,
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
			MaxGateFailures:                  3,
			MaxConsecutiveToolFailures:       5,
			MaxConsecutiveValidationFailures: 5,
			TokenBudget:                      150000,
			MaxToolCalls:                     5,
		},
	}
}
