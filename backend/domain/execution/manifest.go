package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ErrManifestMismatch indicates that a resume request does not use the exact
// execution manifest recorded by its checkpoint.
var ErrManifestMismatch = errors.New("execution manifest mismatch")

// FreezeManifest returns an immutable-by-convention execution manifest with
// deterministic digests. It copies and sorts Tools, leaving the caller's
// RunEnvironment and backing slice unchanged.
func FreezeManifest(environment entity.RunEnvironment) entity.RunEnvironment {
	frozen := environment
	frozen.Tools = append([]string{}, environment.Tools...)
	sort.Strings(frozen.Tools)

	frozen.ModelDigest = canonicalDigest("manifest:model:v1", modelFingerprint{
		ModelName:      frozen.ModelName,
		ModelBaseURL:   frozen.ModelBaseURL,
		ModelRefDigest: frozen.ModelRefDigest,
	})
	frozen.ToolsetDigest = canonicalDigest("manifest:toolset:v1", toolsetFingerprint{
		Tools:              frozen.Tools,
		ToolBindingsDigest: frozen.ToolBindingsDigest,
	})
	frozen.ConfigDigest = canonicalDigest("manifest:config:v1", configFingerprint{
		KnowledgeIndex: frozen.KnowledgeIndex,
		ConfigVersion:  frozen.ConfigVersion,
	})
	frozen.ManifestDigest = canonicalDigest("manifest:v1", manifestFingerprint{
		RuntimeVersion: frozen.RuntimeVersion,
		PromptVersion:  frozen.PromptVersion,
		PromptDigest:   frozen.PromptDigest,
		ModelDigest:    frozen.ModelDigest,
		ToolsetDigest:  frozen.ToolsetDigest,
		ConfigDigest:   frozen.ConfigDigest,
	})
	return frozen
}

// ValidateManifest permits resume only when both non-empty manifest digests
// are identical. Missing digests are rejected so legacy checkpoints cannot
// silently bypass manifest validation.
func ValidateManifest(checkpointDigest string, currentDigest string) error {
	if checkpointDigest == "" || currentDigest == "" || checkpointDigest != currentDigest {
		return fmt.Errorf("%w: checkpoint=%q current=%q", ErrManifestMismatch, checkpointDigest, currentDigest)
	}
	return nil
}

type modelFingerprint struct {
	ModelName      string `json:"model_name"`
	ModelBaseURL   string `json:"model_base_url"`
	ModelRefDigest string `json:"model_ref_digest"`
}

type toolsetFingerprint struct {
	Tools              []string `json:"tools"`
	ToolBindingsDigest string   `json:"tool_bindings_digest"`
}

type configFingerprint struct {
	KnowledgeIndex bool  `json:"knowledge_index"`
	ConfigVersion  int64 `json:"config_version"`
}

type manifestFingerprint struct {
	RuntimeVersion string `json:"runtime_version"`
	PromptVersion  string `json:"prompt_version"`
	PromptDigest   string `json:"prompt_digest"`
	ModelDigest    string `json:"model_digest"`
	ToolsetDigest  string `json:"toolset_digest"`
	ConfigDigest   string `json:"config_digest"`
}

type modelReferenceFingerprint struct {
	ModelID   int64                       `json:"model_id"`
	BaseURL   string                      `json:"base_url"`
	ModelName string                      `json:"model_name"`
	Params    *modelParamsFingerprint     `json:"params"`
	Fallbacks []modelReferenceFingerprint `json:"fallbacks"`
}

type modelParamsFingerprint struct {
	Temperature      *float32              `json:"temperature"`
	MaxTokens        int                   `json:"max_tokens"`
	TopP             *float32              `json:"top_p"`
	TopK             *int32                `json:"top_k"`
	FrequencyPenalty float32               `json:"frequency_penalty"`
	PresencePenalty  float32               `json:"presence_penalty"`
	ResponseFormat   entity.ResponseFormat `json:"response_format"`
	EnableThinking   *bool                 `json:"enable_thinking"`
}

type toolBindingFingerprint struct {
	Source      entity.ToolSource `json:"source"`
	PluginID    int64             `json:"plugin_id"`
	ToolID      int64             `json:"tool_id"`
	IsDraft     bool              `json:"is_draft"`
	ToolName    string            `json:"tool_name"`
	WorkflowID  int64             `json:"workflow_id"`
	Server      string            `json:"server"`
	Reliability *float64          `json:"reliability"`
}

// ModelReferenceDigest identifies the provider/model chain and generation
// parameters without retaining credentials or accounting-only price metadata.
func ModelReferenceDigest(ref entity.ModelRef) string {
	return canonicalDigest("manifest:model-ref:v1", canonicalModelReference(ref))
}

// ToolBindingsDigest identifies the complete configured bindings after sorting
// their stable source-specific fields, so source or server changes cannot reuse
// a checkpoint created under another tool environment.
func ToolBindingsDigest(bindings []entity.ToolBinding) string {
	canonical := make([]toolBindingFingerprint, 0, len(bindings))
	for _, binding := range bindings {
		canonical = append(canonical, toolBindingFingerprint{
			Source: binding.Source, PluginID: binding.PluginID, ToolID: binding.ToolID,
			IsDraft: binding.IsDraft, ToolName: binding.ToolName, WorkflowID: binding.WorkflowID,
			Server: binding.Server, Reliability: binding.Reliability,
		})
	}
	sort.Slice(canonical, func(i, j int) bool {
		left, _ := json.Marshal(canonical[i])
		right, _ := json.Marshal(canonical[j])
		return string(left) < string(right)
	})
	return canonicalDigest("manifest:tool-bindings:v1", canonical)
}

// PromptContentDigest versions the concrete workflow prompt selected for this
// invocation, including prompt-provider overrides and built-in guidance.
func PromptContentDigest(prompt string) string {
	return canonicalDigest("manifest:prompt-content:v1", prompt)
}

func canonicalModelReference(ref entity.ModelRef) modelReferenceFingerprint {
	fingerprint := modelReferenceFingerprint{
		ModelID: ref.ModelID, BaseURL: ref.BaseURL, ModelName: ref.ModelName,
		Fallbacks: make([]modelReferenceFingerprint, len(ref.Fallbacks)),
	}
	if ref.Params != nil {
		fingerprint.Params = &modelParamsFingerprint{
			Temperature: ref.Params.Temperature, MaxTokens: ref.Params.MaxTokens,
			TopP: ref.Params.TopP, TopK: ref.Params.TopK,
			FrequencyPenalty: ref.Params.FrequencyPenalty, PresencePenalty: ref.Params.PresencePenalty,
			ResponseFormat: ref.Params.ResponseFormat, EnableThinking: ref.Params.EnableThinking,
		}
	}
	for i := range ref.Fallbacks {
		fingerprint.Fallbacks[i] = canonicalModelReference(ref.Fallbacks[i])
	}
	return fingerprint
}

func canonicalDigest(prefix string, value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal manifest fingerprint: %v", err))
	}
	return digest(prefix + "|" + string(encoded))
}
