package entity

import "time"

// MagiConfig is the configuration of a single Magi agent (ADR-002). It carries
// agent semantics (persona, objective, risk, evidence standard) and references
// Coze infrastructure by ID (ModelRef, ToolBinding) -- ADR-007: it does NOT
// import modelbuilder; LLMParams is MAGI-owned.
type MagiConfig struct {
	ID   string
	Code string // MagiCode; kept as string for M1/M2 compatibility, migrates to MagiCode in S2
	Name string

	Persona    string             // legacy freeform persona (M1/M2 use it); superseded by PersonaDef
	PersonaDef *PersonaDefinition // structured persona (ADR-002 full)

	Objective        ObjectiveFunction
	RiskTendency     RiskTendency
	RiskPolicy       RiskPolicy
	RolePolicy       RolePolicy
	EvidenceStandard EvidenceStandard

	Model ModelRef
	Tools []ToolBinding

	LoopPolicy       LoopPolicy
	ReflectionPolicy ReflectionPolicy

	Version int64
}

// PersonaDefinition is the structured persona (supersedes the freeform Persona string).
type PersonaDefinition struct {
	SystemPrompt string
	Voice        string
}

// ObjectiveFunction is the explicit value function of a Magi.
type ObjectiveFunction struct {
	Dimensions []UtilityDimension
}

// UtilityDimension is one axis of a Magi's value function.
type UtilityDimension struct {
	Code        string
	Description string
	Weight      float64
}

// RiskTendency expresses a Magi's risk appetite.
type RiskTendency string

const (
	RiskTendencyNeutral            RiskTendency = "neutral"
	RiskTendencyConservative       RiskTendency = "conservative"
	RiskTendencyAggressive         RiskTendency = "aggressive"
	RiskTendencyEvidenceCalibrated RiskTendency = "evidence_calibrated"
)

// RiskPolicy packages risk tendency with an explicit acceptable-risk threshold.
type RiskPolicy struct {
	Tendency          RiskTendency
	MaxAcceptableRisk float64
}

// EvidenceStandard is the deterministic gate policy (ADR-004).
type EvidenceStandard struct {
	MinEvidenceCount      int
	MinQuantitativeCount  int
	MinReliability        float64
	RequireOwnCollected   bool
	RequiredEvidenceTypes []string                  // legacy (M2 gate uses it)
	RequiredTypes         []EvidenceTypeRequirement // ADR-004 expanded
	RequiredClaimCount    int
	CustomRules           []EvidenceRule
}

// EvidenceTypeRequirement is a per-type minimum count for the gate.
type EvidenceTypeRequirement struct {
	Type     string
	MinCount int
}

// EvidenceRule is a deterministic custom gate rule placeholder.
type EvidenceRule struct {
	Code string
	Expr string
}

// ModelRef references a model. Supports two modes:
// - Direct mode (standalone): APIKey + BaseURL + ModelName -> eino-ext openai.NewChatModel
// - Coze mode (integrated): ModelID > 0 -> Coze modelbuilder.BuildModelByID
type ModelRef struct {
	APIKey             string
	BaseURL            string
	ModelName          string  // e.g. "gpt-4o", "doubao-pro-32k"
	PricePerMInputUSD  float64 // USD per million input tokens (cost accounting)
	PricePerMOutputUSD float64 // USD per million output tokens (cost accounting)
	ModelID            int64   // Coze model ID (0 = use direct mode)
	Params             *LLMParams
}

// LLMParams is the MAGI-owned run-time LLM params. The application adapter
// converts this to modelbuilder.LLMParams; domain/magi does NOT import modelbuilder.
type LLMParams struct {
	Temperature      *float32
	MaxTokens        int
	TopP             *float32
	TopK             *int32
	FrequencyPenalty float32
	PresencePenalty  float32
	ResponseFormat   ResponseFormat
	EnableThinking   *bool
}

// ResponseFormat is the requested model response format.
type ResponseFormat string

const (
	ResponseFormatText     ResponseFormat = "text"
	ResponseFormatMarkdown ResponseFormat = "markdown"
	ResponseFormatJSON     ResponseFormat = "json"
)

// ToolBinding references a tool by source. Plugin tools are resolved via Coze
// crossplugin; local tools via the MAGI LocalToolRegistry; others via their ports.
type ToolBinding struct {
	Source      ToolSource
	PluginID    int64  // valid when Source == ToolSourcePlugin
	ToolID      int64  // valid when Source == ToolSourcePlugin
	IsDraft     bool   // draft/online selection when Source == ToolSourcePlugin
	ToolName    string // valid when Source == ToolSourceLocal
	WorkflowID  int64  // valid when Source == ToolSourceWorkflow
	Reliability *float64
}

// ToolSource is the origin of a bound tool.
type ToolSource string

const (
	ToolSourcePlugin     ToolSource = "plugin"
	ToolSourceLocal      ToolSource = "local"
	ToolSourceKnowledge  ToolSource = "knowledge"
	ToolSourceWorkflow   ToolSource = "workflow"
	ToolSourceCodeRunner ToolSource = "coderunner"
)

// LoopPolicy bounds a single agent run.
type LoopPolicy struct {
	MaxSteps                         int
	Timeout                          time.Duration // total loop budget for the agent run
	CallTimeout                      time.Duration // per chat completion cap (0 = inherit loop timeout)
	MaxGateFailures                  int
	MaxConsecutiveToolFailures       int
	MaxConsecutiveValidationFailures int
	TokenBudget                      int
	MaxToolCalls                     int // 0 = unlimited; >0 forces convergence to EvidenceSummary after N tool calls
}

// ReflectionPolicy bounds the debate/reflection phase.
type ReflectionPolicy struct {
	MaxRounds            int
	RequireNewEvidence   bool
	RequireJustification bool
}
