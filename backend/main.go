package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
)

// --- YAML config structs ---

type Config struct {
	Model struct {
		APIKey    string `yaml:"api_key"`
		BaseURL   string `yaml:"base_url"`
		ModelName string `yaml:"model_name"`
	} `yaml:"model"`
	Magi struct {
		MaxDebateRounds int `yaml:"max_debate_rounds"`
		MaxSteps        int `yaml:"max_steps"`
		TimeoutSeconds  int `yaml:"timeout_seconds"`
		Melchior        MagiSpec `yaml:"melchior"`
		Balthasar       MagiSpec `yaml:"balthasar"`
		Casper          MagiSpec `yaml:"casper"`
	} `yaml:"magi"`
}

type MagiSpec struct {
	Persona      string             `yaml:"persona"`
	Dimensions   []DimensionSpec    `yaml:"dimensions"`
	RiskTendency string             `yaml:"risk_tendency"`
	Evidence     EvidenceSpec       `yaml:"evidence"`
}

type DimensionSpec struct {
	Code        string  `yaml:"code"`
	Description string  `yaml:"description"`
	Weight      float64 `yaml:"weight"`
}

type EvidenceSpec struct {
	MinEvidenceCount     int           `yaml:"min_evidence_count"`
	MinQuantitativeCount int           `yaml:"min_quantitative_count"`
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

func loadConfig(path string) (*Config, error) {
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

func (s *MagiSpec) toConfig(code string, cfg *Config) *entity.MagiConfig {
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
	return &entity.MagiConfig{
		Code:         code,
		Persona:      s.Persona,
		Objective:    entity.ObjectiveFunction{Dimensions: dims},
		RiskTendency: entity.RiskTendency(s.RiskTendency),
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount:     s.Evidence.MinEvidenceCount,
			MinQuantitativeCount: s.Evidence.MinQuantitativeCount,
			RequiredClaimCount:   s.Evidence.RequiredClaimCount,
			RequiredTypes:        reqTypes,
			CustomRules:          customRules,
		},
		Model: entity.ModelRef{
			APIKey:    cfg.Model.APIKey,
			BaseURL:   cfg.Model.BaseURL,
			ModelName: cfg.Model.ModelName,
		},
		LoopPolicy: entity.LoopPolicy{MaxSteps: cfg.Magi.MaxSteps, Timeout: time.Duration(cfg.Magi.TimeoutSeconds) * time.Second},
	}
}

// --- stubs for standalone CLI ---

type stubToolRegistry struct{}

func (s *stubToolRegistry) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	return nil, nil
}

type stubToolExecutor struct{}

func (s *stubToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	return &port.ToolExecutionResult{Output: "stub result"}, nil
}

// --- main ---

func main() {
	configPath := "conf/magi.yaml"
	if p := os.Getenv("MAGI_CONFIG"); p != "" {
		configPath = p
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config from %s: %v\n", configPath, err)
		os.Exit(1)
	}

	question := "是否应该把后端从 Java 重构成 Rust？"
	if len(os.Args) > 1 {
		question = os.Args[1]
	}

	fmt.Printf("MAGI Decision Engine\n")
	fmt.Printf("Question: %s\n", question)
	fmt.Printf("Model: %s\n\n", cfg.Model.ModelName)

	// 1. Validation
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()

	// 2. Adapters
	modelAdapter := magi.NewModelAdapter()
	toolReg := &stubToolRegistry{}
	toolExec := &stubToolExecutor{}
	eventPub := magi.NewEventPublisherAdapter(magi.NewInMemoryEventRepo())

	// 3. AgentLoop
	agentLoop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelAdapter, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen,
		EventPub: eventPub,
	})
	if err != nil {
		fmt.Printf("Failed to create agent loop: %v\n", err)
		os.Exit(1)
	}

	// 4. Commander
	commander, err := service.NewCommander(
		service.CommanderConfig{
			Model:   entity.ModelRef{APIKey: cfg.Model.APIKey, BaseURL: cfg.Model.BaseURL, ModelName: cfg.Model.ModelName},
			Persona: "commander",
		},
		modelAdapter, gen, val,
	)
	if err != nil {
		fmt.Printf("Failed to create commander: %v\n", err)
		os.Exit(1)
	}

	// 5. Magi configs from YAML
	configs := []*entity.MagiConfig{
		cfg.Magi.Melchior.toConfig("melchior", cfg),
		cfg.Magi.Balthasar.toConfig("balthasar", cfg),
		cfg.Magi.Casper.toConfig("casper", cfg),
	}

	// 6. Orchestrator
	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: agentLoop,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: commander,
		EventPub:  eventPub,
		Configs:   configs,
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	// 7. Run
	case_ := &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%d", time.Now().Unix()),
		Question:        question,
		MaxDebateRounds: cfg.Magi.MaxDebateRounds,
		Status:          entity.CaseStatusDraft,
		CreatedAt:       time.Now(),
	}

	ctx := context.Background()
	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		fmt.Printf("\nDecision failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== Resolution ===\n")
	fmt.Printf("Consensus: %s (round %d)\n", res.Consensus.Outcome, res.Consensus.Round)
	fmt.Printf("Decision: %s\n", res.FinalDecision)
	fmt.Printf("\nReport:\n%s\n", res.FinalReport)
}
