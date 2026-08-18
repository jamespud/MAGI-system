package magi

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
)

type fakeProviderModel struct {
	generate func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error)
	stream   func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error)
	toolsErr error
	tools    []*schema.ToolInfo
}

func (f *fakeProviderModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if f.generate == nil {
		return &schema.Message{Content: "ok"}, nil
	}
	return f.generate(ctx, input, opts...)
}

func (f *fakeProviderModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if f.stream == nil {
		return schema.StreamReaderFromArray([]*schema.Message{{Content: "ok"}}), nil
	}
	return f.stream(ctx, input, opts...)
}

func (f *fakeProviderModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if f.toolsErr != nil {
		return nil, f.toolsErr
	}
	f.tools = append([]*schema.ToolInfo(nil), tools...)
	return f, nil
}

func TestModelAdapterBuildSkipsInvalidPrimaryAndUsesFallback(t *testing.T) {
	reg := metrics.New()
	adapter := NewModelAdapterWithMetrics(reg)
	got, err := adapter.Build(context.Background(), entity.ModelRef{
		Fallbacks: []entity.ModelRef{{APIKey: "fallback-key", ModelName: "fallback-model"}},
	})
	if err != nil {
		t.Fatalf("build provider chain: %v", err)
	}
	if got == nil {
		t.Fatal("expected fallback model")
	}
	if reg.ModelFailovers.Load() != 1 {
		t.Fatalf("build failover metric = %d, want 1", reg.ModelFailovers.Load())
	}
}

func TestFailoverChatModelGenerateMovesToNextProvider(t *testing.T) {
	reg := metrics.New()
	primary := errors.New("primary unavailable")
	called := false
	providers := []model.ToolCallingChatModel{
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			called = true
			return nil, primary
		}},
		&fakeProviderModel{},
	}
	wrapper, err := NewFailoverChatModel(providers, []entity.ModelRef{
		{ModelName: "primary", BaseURL: "https://primary"},
		{ModelName: "fallback", BaseURL: "https://fallback"},
	}, reg)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	got, err := wrapper.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got.Content != "ok" || !called {
		t.Fatalf("fallback was not used: called=%v message=%+v", called, got)
	}
	if reg.ModelFailovers.Load() != 1 {
		t.Fatalf("failover metric = %d, want 1", reg.ModelFailovers.Load())
	}
}

func TestFailoverChatModelAnnotatesProviderCost(t *testing.T) {
	response := &schema.Message{
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		}},
	}
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			return nil, errors.New("primary unavailable")
		}},
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			return response, nil
		}},
	}, []entity.ModelRef{
		{ModelName: "primary"},
		{ModelName: "fallback", PricePerMInputUSD: 2, PricePerMOutputUSD: 4},
	}, nil)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	got, err := wrapper.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cost, ok := got.Extra[entity.ModelCostExtraKey].(float64)
	if !ok || math.Abs(cost-0.00028) > 1e-12 {
		t.Fatalf("provider cost = %v (%T), want 0.00028", got.Extra[entity.ModelCostExtraKey], got.Extra[entity.ModelCostExtraKey])
	}
}

func TestFailoverChatModelReturnsJoinedErrors(t *testing.T) {
	primaryErr := errors.New("primary down")
	fallbackErr := errors.New("fallback down")
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			return nil, primaryErr
		}},
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			return nil, fallbackErr
		}},
	}, []entity.ModelRef{{ModelName: "primary"}, {ModelName: "fallback"}}, nil)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	if _, err := wrapper.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected joined provider errors")
	} else if !errors.Is(err, primaryErr) || !errors.Is(err, fallbackErr) {
		t.Fatalf("joined error does not preserve causes: %v", err)
	}
}

func TestFailoverChatModelDoesNotRetryCanceledContext(t *testing.T) {
	fallbackCalled := false
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{},
		&fakeProviderModel{generate: func(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
			fallbackCalled = true
			return &schema.Message{Content: "should not run"}, nil
		}},
	}, []entity.ModelRef{{ModelName: "primary"}, {ModelName: "fallback"}}, nil)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wrapper.Generate(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if fallbackCalled {
		t.Fatal("canceled request must not invoke fallback")
	}
}

func TestFailoverChatModelWithToolsSkipsUnbindableProvider(t *testing.T) {
	primary := &fakeProviderModel{toolsErr: errors.New("tool schema unsupported")}
	fallback := &fakeProviderModel{}
	wrapper, err := NewFailoverChatModel(
		[]model.ToolCallingChatModel{primary, fallback},
		[]entity.ModelRef{{ModelName: "primary"}, {ModelName: "fallback"}},
		metrics.New(),
	)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	tools := []*schema.ToolInfo{{Name: "search"}}
	bound, err := wrapper.WithTools(tools)
	if err != nil {
		t.Fatalf("bind tools: %v", err)
	}
	if _, err := bound.Generate(context.Background(), nil); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(fallback.tools) != 1 || fallback.tools[0].Name != "search" {
		t.Fatalf("fallback did not receive tools: %+v", fallback.tools)
	}
}

func TestFailoverChatModelStreamSkipsImmediateFailure(t *testing.T) {
	reg := metrics.New()
	brokenStream := func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
		reader, writer := schema.Pipe[*schema.Message](1)
		go func() {
			defer writer.Close()
			writer.Send(nil, errors.New("first stream unavailable"))
		}()
		return reader, nil
	}
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{stream: brokenStream},
		&fakeProviderModel{stream: func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			return nil, errors.New("second stream unavailable")
		}},
		&fakeProviderModel{},
	}, []entity.ModelRef{{ModelName: "first"}, {ModelName: "second"}, {ModelName: "third"}}, reg)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	reader, err := wrapper.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer reader.Close()
	if chunk, err := reader.Recv(); err != nil || chunk.Content != "ok" {
		t.Fatalf("third provider was not used: chunk=%+v err=%v", chunk, err)
	}
	if reg.ModelFailovers.Load() != 2 {
		t.Fatalf("failover metric = %d, want 2", reg.ModelFailovers.Load())
	}
}

func TestFailoverChatModelStreamDoesNotRetryAfterFirstChunk(t *testing.T) {
	reg := metrics.New()
	partialStream := func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
		reader, writer := schema.Pipe[*schema.Message](1)
		go func() {
			defer writer.Close()
			writer.Send(&schema.Message{Content: "partial"}, nil)
			writer.Send(nil, errors.New("disconnected after first chunk"))
		}()
		return reader, nil
	}
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{stream: partialStream},
		&fakeProviderModel{},
	}, []entity.ModelRef{{ModelName: "primary"}, {ModelName: "fallback"}}, reg)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	reader, err := wrapper.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer reader.Close()
	if chunk, err := reader.Recv(); err != nil || chunk.Content != "partial" {
		t.Fatalf("first chunk = %+v, %v; want primary partial output", chunk, err)
	}
	if _, err := reader.Recv(); err == nil || err.Error() != "disconnected after first chunk" {
		t.Fatalf("second receive error = %v, want surfaced mid-stream failure", err)
	}
	if reg.ModelFailovers.Load() != 0 {
		t.Fatalf("mid-stream retry must not run; metric=%d", reg.ModelFailovers.Load())
	}
}

func TestFailoverChatModelStreamMovesOverBeforeFirstChunk(t *testing.T) {
	reg := metrics.New()
	primaryStream := func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
		reader, writer := schema.Pipe[*schema.Message](1)
		go func() {
			defer writer.Close()
			writer.Send(nil, errors.New("primary stream unavailable"))
		}()
		return reader, nil
	}
	wrapper, err := NewFailoverChatModel([]model.ToolCallingChatModel{
		&fakeProviderModel{stream: primaryStream},
		&fakeProviderModel{},
	}, []entity.ModelRef{{ModelName: "primary"}, {ModelName: "fallback"}}, reg)
	if err != nil {
		t.Fatalf("build wrapper: %v", err)
	}
	reader, err := wrapper.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer reader.Close()
	chunk, err := reader.Recv()
	if err != nil || chunk.Content != "ok" {
		t.Fatalf("first chunk = %+v, %v; want fallback output", chunk, err)
	}
	if _, err := reader.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream end = %v, want EOF", err)
	}
	if reg.ModelFailovers.Load() != 1 {
		t.Fatalf("failover metric = %d, want 1", reg.ModelFailovers.Load())
	}
}
