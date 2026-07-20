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
}

type MagiSpec struct {
	Persona          string               `yaml:"persona"`
	PersonaDef       PersonaDefSpec       `yaml:"persona_def"`
	Dimensions       []DimensionSpec      `yaml:"dimensions"`
	RiskTendency     string               `yaml:"risk_tendency"`
	RiskPolicy       RiskPolicySpec       `yaml:"risk_policy"`
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
	return &cfg, nil
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
		LoopPolicy: entity.LoopPolicy{
			MaxSteps:                         cfg.Magi.MaxSteps,
			Timeout:                          time.Duration(cfg.Magi.TimeoutSeconds) * time.Second,
			MaxGateFailures:                  3,
			MaxConsecutiveToolFailures:       5,
			MaxConsecutiveValidationFailures: 5,
			TokenBudget:                      50000,
		},
	}
}
