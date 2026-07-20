package magi

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	bot_common "github.com/coze-dev/coze-studio/backend/api/model/app/bot_common"
	"github.com/coze-dev/coze-studio/backend/bizpkg/llm/modelbuilder"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ModelAdapter implements port.ModelPort with dual-mode model building.
// - Direct mode: APIKey + ModelName -> eino-ext openai.NewChatModel (standalone)
// - Coze mode: ModelID > 0 -> modelbuilder.BuildModelByID (integrated)
type ModelAdapter struct{}

func NewModelAdapter() *ModelAdapter { return &ModelAdapter{} }

func (a *ModelAdapter) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	if ref.APIKey != "" {
		return a.buildDirect(ctx, ref)
	}
	if ref.ModelID > 0 {
		return a.buildViaCoze(ctx, ref)
	}
	return nil, fmt.Errorf("no model config: set APIKey+ModelName (direct) or ModelID (coze)")
}

// buildDirect uses eino-ext openai.NewChatModel (OpenAI-compatible API).
func (a *ModelAdapter) buildDirect(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	if ref.ModelName == "" {
		return nil, fmt.Errorf("model name is empty")
	}
	conf := &openai.ChatModelConfig{
		APIKey:  ref.APIKey,
		BaseURL: ref.BaseURL,
		Model:   ref.ModelName,
	}
	if ref.Params != nil {
		conf.Temperature = ref.Params.Temperature
		if ref.Params.MaxTokens > 0 {
			conf.MaxTokens = &ref.Params.MaxTokens
		}
		conf.TopP = ref.Params.TopP
	}
	cm, err := openai.NewChatModel(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("build model %q failed: %w", ref.ModelName, err)
	}
	return cm, nil
}

// buildViaCoze uses Coze's modelbuilder (requires Coze config initialized).
func (a *ModelAdapter) buildViaCoze(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	cm, _, err := modelbuilder.BuildModelByID(ctx, ref.ModelID, toModelbuilderParams(ref.Params))
	if err != nil {
		return nil, fmt.Errorf("coze build model by id %d failed: %w", ref.ModelID, err)
	}
	if cm == nil {
		return nil, fmt.Errorf("coze build model by id %d returned nil", ref.ModelID)
	}
	return cm, nil
}

func toModelbuilderParams(p *entity.LLMParams) *modelbuilder.LLMParams {
	if p == nil {
		return nil
	}
	return &modelbuilder.LLMParams{
		Temperature:      p.Temperature,
		FrequencyPenalty: p.FrequencyPenalty,
		PresencePenalty:  p.PresencePenalty,
		MaxTokens:        p.MaxTokens,
		TopP:             p.TopP,
		TopK:             p.TopK,
		ResponseFormat:   toModelbuilderResponseFormat(p.ResponseFormat),
		EnableThinking:   p.EnableThinking,
	}
}

func toModelbuilderResponseFormat(r entity.ResponseFormat) bot_common.ModelResponseFormat {
	switch r {
	case entity.ResponseFormatJSON:
		return bot_common.ModelResponseFormat_JSON
	case entity.ResponseFormatMarkdown:
		return bot_common.ModelResponseFormat_Markdown
	default:
		return bot_common.ModelResponseFormat_Text
	}
}

var _ port.ModelPort = (*ModelAdapter)(nil)
