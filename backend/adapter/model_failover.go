package magi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
)

// failoverChatModel tries providers in order. Generation failures move to the
// next provider before the error reaches domain orchestration. Streaming can
// fail over until the first chunk has been emitted; after that, retrying would
// duplicate partial output, so the error is surfaced instead.
type failoverChatModel struct {
	candidates []model.ToolCallingChatModel
	refs       []entity.ModelRef
	metrics    *metrics.Registry
}

// NewFailoverChatModel wraps already-built models in provider order. It is
// exported for direct adapter tests; production callers receive it from
// ModelAdapter.Build.
func NewFailoverChatModel(
	candidates []model.ToolCallingChatModel,
	refs []entity.ModelRef,
	reg *metrics.Registry,
) (model.ToolCallingChatModel, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("model failover chain is empty")
	}
	if len(candidates) != len(refs) {
		return nil, fmt.Errorf("model failover chain mismatch: %d models and %d refs", len(candidates), len(refs))
	}
	return &failoverChatModel{
		candidates: append([]model.ToolCallingChatModel(nil), candidates...),
		refs:       append([]entity.ModelRef(nil), refs...),
		metrics:    reg,
	}, nil
}

func (m *failoverChatModel) Generate(
	ctx context.Context,
	input []*schema.Message,
	opts ...model.Option,
) (*schema.Message, error) {
	var errs []error
	for i, candidate := range m.candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		output, err := candidate.Generate(ctx, input, opts...)
		if err == nil {
			annotateProviderCost(output, m.refs[i])
			return output, nil
		}
		errs = append(errs, fmt.Errorf("provider %s: %w", modelRefLabel(m.refs[i]), err))
		if ctx.Err() != nil || i == len(m.candidates)-1 {
			break
		}
		m.reportFailover(ctx, i, err)
	}
	return nil, errors.Join(errs...)
}

func (m *failoverChatModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	var errs []error
	start := -1
	var active *schema.StreamReader[*schema.Message]

	for i, candidate := range m.candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reader, err := candidate.Stream(ctx, input, opts...)
		if err == nil {
			start, active = i, reader
			break
		}
		errs = append(errs, fmt.Errorf("provider %s: %w", modelRefLabel(m.refs[i]), err))
		if ctx.Err() != nil || i == len(m.candidates)-1 {
			break
		}
		m.reportFailover(ctx, i, err)
	}
	if active == nil {
		return nil, errors.Join(errs...)
	}

	output, writer := schema.Pipe[*schema.Message](1)
	go m.pumpStream(ctx, writer, active, start, input, opts...)
	return output, nil
}

func (m *failoverChatModel) pumpStream(
	ctx context.Context,
	writer *schema.StreamWriter[*schema.Message],
	active *schema.StreamReader[*schema.Message],
	start int,
	input []*schema.Message,
	opts ...model.Option,
) {
	defer writer.Close()
	delivered := false

	for i := start; i < len(m.candidates); i++ {
		if i > start {
			if ctx.Err() != nil {
				_ = writer.Send(nil, ctx.Err())
				return
			}
			reader, err := m.candidates[i].Stream(ctx, input, opts...)
			if err != nil {
				if i == len(m.candidates)-1 || ctx.Err() != nil {
					_ = writer.Send(nil, err)
					return
				}
				m.reportFailover(ctx, i, err)
				continue
			}
			active = reader
		}

		for {
			chunk, err := active.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				active.Close()
				if delivered || i == len(m.candidates)-1 || ctx.Err() != nil {
					_ = writer.Send(nil, err)
					return
				}
				m.reportFailover(ctx, i, err)
				break
			}
			delivered = true
			annotateProviderCost(chunk, m.refs[i])
			if writer.Send(chunk, nil) {
				active.Close()
				return
			}
		}
	}
}

func (m *failoverChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	var candidates []model.ToolCallingChatModel
	var refs []entity.ModelRef
	var errs []error

	for i, candidate := range m.candidates {
		bound, err := candidate.WithTools(tools)
		if err != nil {
			errs = append(errs, fmt.Errorf("provider %s: %w", modelRefLabel(m.refs[i]), err))
			if i < len(m.candidates)-1 {
				m.reportFailover(context.Background(), i, err)
			}
			continue
		}
		candidates = append(candidates, bound)
		refs = append(refs, m.refs[i])
	}
	if len(candidates) == 0 {
		return nil, errors.Join(errs...)
	}
	return &failoverChatModel{candidates: candidates, refs: refs, metrics: m.metrics}, nil
}

func (m *failoverChatModel) reportFailover(ctx context.Context, failedIndex int, err error) {
	if m.metrics != nil {
		m.metrics.IncModelFailover()
	}
	if err == nil {
		return
	}
	next := "none"
	if failedIndex+1 < len(m.refs) {
		next = modelRefLabel(m.refs[failedIndex+1])
	}
	log.Printf("model provider %s failed, failing over to %s (request context=%v): %v",
		modelRefLabel(m.refs[failedIndex]), next, errValue(ctx.Err()), err)
}

func modelRefLabel(ref entity.ModelRef) string {
	if ref.ModelName != "" {
		if ref.BaseURL != "" {
			return ref.ModelName + "@" + ref.BaseURL
		}
		return ref.ModelName
	}
	if ref.ModelID > 0 {
		return fmt.Sprintf("coze-model-%d", ref.ModelID)
	}
	return "unnamed-model"
}

func errValue(err error) any {
	if err == nil {
		return nil
	}
	return err
}

var _ model.ToolCallingChatModel = (*failoverChatModel)(nil)

func annotateProviderCost(msg *schema.Message, ref entity.ModelRef) {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return
	}
	tokenUsage := msg.ResponseMeta.Usage
	usage := &entity.Usage{
		PromptTokens:     int64(tokenUsage.PromptTokens),
		CompletionTokens: int64(tokenUsage.CompletionTokens),
		TotalTokens:      int64(tokenUsage.TotalTokens),
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any)
	}
	msg.Extra[entity.ModelCostExtraKey] = usage.Cost(ref.PricePerMInputUSD, ref.PricePerMOutputUSD)
}
