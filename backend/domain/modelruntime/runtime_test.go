package modelruntime_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/modelruntime"
)

func TestModelRuntimeResumeDoesNotRegenerate(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	firstResponse := &schema.Message{
		Role:             schema.Assistant,
		Content:          "response A",
		ReasoningContent: "reasoning A",
		MultiContent: []schema.ChatMessagePart{
			{Type: schema.ChatMessagePartTypeText, Text: "summary"},
			{Type: schema.ChatMessagePartTypeImageURL, ImageURL: &schema.ChatMessageImageURL{
				URL:   "https://example.test/image.png",
				Extra: map[string]any{"rank": int64(9)},
			}},
		},
		Extra: map[string]any{
			"int64":  int64(42),
			"uint32": uint32(7),
			"bytes":  []byte{0, 1, 2},
			"nested": map[string]any{"attempt": int32(3)},
		},
		ToolCalls: []schema.ToolCall{{
			ID:   "call-a",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "search",
				Arguments: `{"query":"MAGI"}`,
			},
			Extra: map[string]any{"retry": int16(2)},
		}},
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: "tool_calls",
			Usage:        &schema.TokenUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		},
	}
	secondResponse := schema.AssistantMessage("response B", nil)
	provider := &scriptedModel{responses: []*schema.Message{firstResponse, secondResponse}}
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	messages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}

	first, err := runtime.Generate(context.Background(), modelruntime.Request{
		Identity: entity.ExecutionIdentity{
			RunID: "run-1", StepID: stepID,
			InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1",
		},
		ModelRef: modelRef,
		Model:    provider,
		Input:    messages,
	})
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if !reflect.DeepEqual(first, firstResponse) {
		t.Fatalf("first response = %#v, want %#v", first, firstResponse)
	}

	resumed, err := runtime.Generate(context.Background(), modelruntime.Request{
		Identity: entity.ExecutionIdentity{
			RunID: "run-1", StepID: stepID,
			InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-2",
		},
		ModelRef: modelRef,
		Model:    provider,
		Input:    messages,
	})
	if err != nil {
		t.Fatalf("resume generate: %v", err)
	}
	if !reflect.DeepEqual(resumed, firstResponse) {
		t.Fatalf("resumed response = %#v, want persisted %#v", resumed, firstResponse)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeEquivalentInputMapsReuseCachedResponse(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("response A", nil),
		schema.AssistantMessage("response B", nil),
	}}

	first, err := runtime.Generate(context.Background(), modelruntime.Request{
		Identity: identity, ModelRef: modelRef, Model: provider, Input: equivalentInputMessages(false),
	})
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	identity.AttemptID = "attempt-2"
	resumed, err := runtime.Generate(context.Background(), modelruntime.Request{
		Identity: identity, ModelRef: modelRef, Model: provider, Input: equivalentInputMessages(true),
	})
	if err != nil {
		t.Fatalf("resume generate: %v", err)
	}
	if first.Content != "response A" || resumed.Content != "response A" {
		t.Fatalf("responses = first=%q resumed=%q, want persisted response A", first.Content, resumed.Content)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeRoundOneInputReusesSucceededResponse(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("response A", nil),
		schema.AssistantMessage("response B", nil),
	}}
	messages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	repo.replaceInput(identity.InvocationID, legacyRoundOneInput(t, modelRef, messages))
	identity.AttemptID = "attempt-2"
	resumed, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages})
	if err != nil {
		t.Fatalf("resume generate: %v", err)
	}
	if resumed.Content != "response A" {
		t.Fatalf("resumed response = %q, want persisted response A", resumed.Content)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeRoundOneInputRejectsChangedRequest(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("response A", nil),
		schema.AssistantMessage("response B", nil),
	}}
	messages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	repo.replaceInput(identity.InvocationID, legacyRoundOneInput(t, modelRef, messages))
	identity.AttemptID = "attempt-2"
	changedMessages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("changed question")}
	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: changedMessages}); !errors.Is(err, execution.ErrInvocationMismatch) {
		t.Fatalf("resume generate error = %v, want immutable input mismatch", err)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeRoundOneInputRejectsInvalidPersistedDigest(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("response A", nil),
		schema.AssistantMessage("response B", nil),
	}}
	messages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	repo.replaceInput(identity.InvocationID, legacyRoundOneInput(t, modelRef, messages))
	repo.replaceInputDigest(identity.InvocationID, "invalid")
	identity.AttemptID = "attempt-2"
	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); !errors.Is(err, execution.ErrInvocationMismatch) {
		t.Fatalf("resume generate error = %v, want immutable input mismatch", err)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeDefinedContainerTypesDoNotCollide(t *testing.T) {
	type firstMap map[string]any
	type secondMap map[string]any
	type firstSlice []string
	type secondSlice []string
	type firstArray [1]string
	type secondArray [1]string

	for _, test := range []struct {
		name   string
		first  any
		second any
	}{
		{name: "map", first: firstMap{"key": "value"}, second: secondMap{"key": "value"}},
		{name: "slice", first: firstSlice{"value"}, second: secondSlice{"value"}},
		{name: "array", first: firstArray{"value"}, second: secondArray{"value"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &memoryInvocationRepository{}
			runtime := modelruntime.New(execution.NewKernel(repo, nil))
			stepID := execution.NewStepID("run-1", 1)
			modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
			identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
			provider := &scriptedModel{responses: []*schema.Message{
				schema.AssistantMessage("response A", nil),
				schema.AssistantMessage("response B", nil),
			}}

			firstInput := []*schema.Message{{Role: schema.User, Content: "question", Extra: map[string]any{"value": test.first}}}
			if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: firstInput}); err != nil {
				t.Fatalf("first generate: %v", err)
			}
			identity.AttemptID = "attempt-2"
			secondInput := []*schema.Message{{Role: schema.User, Content: "question", Extra: map[string]any{"value": test.second}}}
			if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: secondInput}); !errors.Is(err, execution.ErrInvocationMismatch) {
				t.Fatalf("second generate error = %v, want immutable input mismatch", err)
			}
			if provider.calls != 1 {
				t.Fatalf("model call count = %d, want 1", provider.calls)
			}
		})
	}
}

func TestModelRuntimeDifferentModelsDoNotReuseResponse(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	firstRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	secondRef := entity.ModelRef{ModelID: 2, ModelName: "model-b"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, firstRef), AttemptID: "attempt-1"}
	firstModel := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("response A", nil)}}
	secondModel := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("response B", nil)}}
	messages := []*schema.Message{schema.UserMessage("question")}

	first, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: firstRef, Model: firstModel, Input: messages})
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if first.Content != "response A" {
		t.Fatalf("first response = %q, want response A", first.Content)
	}

	identity.InvocationID = modelruntime.NewInvocationID(stepID, secondRef)
	identity.AttemptID = "attempt-2"
	second, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: secondRef, Model: secondModel, Input: messages})
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Content != "response B" {
		t.Fatalf("second response = %q, want response B", second.Content)
	}
	if secondModel.calls != 1 {
		t.Fatalf("second model call count = %d, want 1", secondModel.calls)
	}
}

func TestModelRuntimeRetryUsesNewAttemptID(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &retryScriptedModel{err: errors.New("temporary provider failure"), response: schema.AssistantMessage("response A", nil)}
	messages := []*schema.Message{schema.UserMessage("question")}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); err == nil {
		t.Fatal("first generate error = nil, want provider failure")
	}
	identity.AttemptID = "attempt-2"
	response, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages})
	if err != nil {
		t.Fatalf("retry generate: %v", err)
	}
	if response.Content != "response A" {
		t.Fatalf("retry response = %q, want response A", response.Content)
	}
	if got := repo.attemptsFor(identity.InvocationID); !reflect.DeepEqual(got, []string{"attempt-1", "attempt-2"}) {
		t.Fatalf("attempt IDs = %v, want distinct physical attempts", got)
	}
}

func TestModelRuntimeUnsupportedExtraFailsClosed(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{
		{Role: schema.Assistant, Content: "unsupported", Extra: map[string]any{"channel": make(chan int)}},
		schema.AssistantMessage("must not run", nil),
	}}
	messages := []*schema.Message{schema.UserMessage("question")}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("first generate error = %v, want ambiguous invocation", err)
	}
	identity.AttemptID = "attempt-2"
	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("resume generate error = %v, want ambiguous invocation", err)
	}
	if provider.calls != 1 {
		t.Fatalf("model call count = %d, want 1", provider.calls)
	}
}

func TestModelRuntimeUnsupportedInputExtraFailsClosedBeforeProvider(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("must not run", nil)}}
	messages := []*schema.Message{{Role: schema.User, Content: "question", Extra: map[string]any{"channel": make(chan int)}}}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); !errors.Is(err, modelruntime.ErrInvalidRequest) {
		t.Fatalf("generate error = %v, want invalid request", err)
	}
	if provider.calls != 0 {
		t.Fatalf("model call count = %d, want 0", provider.calls)
	}
}

func TestModelRuntimeCyclicInputFailsClosedBeforeProvider(t *testing.T) {
	repo := &memoryInvocationRepository{}
	runtime := modelruntime.New(execution.NewKernel(repo, nil))
	stepID := execution.NewStepID("run-1", 1)
	modelRef := entity.ModelRef{ModelID: 1, ModelName: "model-a"}
	identity := entity.ExecutionIdentity{RunID: "run-1", StepID: stepID, InvocationID: modelruntime.NewInvocationID(stepID, modelRef), AttemptID: "attempt-1"}
	provider := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("must not run", nil)}}
	cycle := map[string]any{}
	cycle["self"] = cycle
	messages := []*schema.Message{{Role: schema.User, Content: "question", Extra: map[string]any{"cycle": cycle}}}

	if _, err := runtime.Generate(context.Background(), modelruntime.Request{Identity: identity, ModelRef: modelRef, Model: provider, Input: messages}); !errors.Is(err, modelruntime.ErrInvalidRequest) {
		t.Fatalf("generate error = %v, want invalid request", err)
	}
	if provider.calls != 0 {
		t.Fatalf("model call count = %d, want 0", provider.calls)
	}
}

func equivalentInputMessages(reverse bool) []*schema.Message {
	nested := make(map[string]any)
	extra := make(map[string]any)
	partExtra := make(map[string]any)
	if reverse {
		nested["second"] = int64(2)
		nested["first"] = int64(1)
		extra["nested"] = nested
		extra["request"] = "same"
		partExtra["height"] = int64(200)
		partExtra["width"] = int64(100)
	} else {
		nested["first"] = int64(1)
		nested["second"] = int64(2)
		extra["request"] = "same"
		extra["nested"] = nested
		partExtra["width"] = int64(100)
		partExtra["height"] = int64(200)
	}
	return []*schema.Message{{
		Role:    schema.User,
		Content: "question",
		Extra:   extra,
		MultiContent: []schema.ChatMessagePart{{
			Type: schema.ChatMessagePartTypeImageURL,
			ImageURL: &schema.ChatMessageImageURL{
				URL:   "https://example.test/image.png",
				Extra: partExtra,
			},
		}},
	}}
}

func legacyRoundOneInput(t *testing.T, modelRef entity.ModelRef, messages []*schema.Message) string {
	t.Helper()
	type legacyParams struct {
		Temperature      *float32
		MaxTokens        int
		TopP             *float32
		TopK             *int32
		FrequencyPenalty float32
		PresencePenalty  float32
		ResponseFormat   entity.ResponseFormat
		EnableThinking   *bool
	}
	type legacyModelIdentity struct {
		ModelID   int64
		BaseURL   string
		ModelName string
		Params    *legacyParams
		Fallbacks []legacyModelIdentity
	}
	type legacyInputEnvelope struct {
		Version  string
		Model    legacyModelIdentity
		Messages []*schema.Message
	}
	model := legacyModelIdentity{
		ModelID: modelRef.ModelID, BaseURL: modelRef.BaseURL, ModelName: modelRef.ModelName,
		Fallbacks: make([]legacyModelIdentity, len(modelRef.Fallbacks)),
	}
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(legacyInputEnvelope{Version: "v1", Model: model, Messages: messages}); err != nil {
		t.Fatalf("encode round-one input: %v", err)
	}
	return "magi:model-input:gob:v1:" + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

type scriptedModel struct {
	responses []*schema.Message
	calls     int
}

func (m *scriptedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	response := m.responses[m.calls]
	m.calls++
	return response, nil
}

func (m *scriptedModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type retryScriptedModel struct {
	err      error
	response *schema.Message
	calls    int
}

func (m *retryScriptedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return nil, m.err
	}
	return m.response, nil
}

func (m *retryScriptedModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *retryScriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type memoryInvocationRepository struct {
	mu          sync.Mutex
	invocations map[string]*entity.RuntimeInvocation
	attemptIDs  map[string]string
	attempts    map[string][]string
}

func (r *memoryInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocations == nil {
		r.invocations = make(map[string]*entity.RuntimeInvocation)
		r.attemptIDs = make(map[string]string)
		r.attempts = make(map[string][]string)
	}
	if r.invocations[invocation.InvocationID] == nil {
		copy := *invocation
		r.invocations[invocation.InvocationID] = &copy
	}
	return cloneInvocation(r.invocations[invocation.InvocationID]), nil
}

func (r *memoryInvocationRepository) Get(_ context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneInvocation(r.invocations[invocationID]), nil
}

func (r *memoryInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.invocations[invocationID]
	if invocation == nil || invocation.Status == entity.InvocationSucceeded {
		return cloneInvocation(invocation), false, nil
	}
	invocation.Status = entity.InvocationRunning
	r.attemptIDs[invocationID] = attemptID
	r.attempts[invocationID] = append(r.attempts[invocationID], attemptID)
	return cloneInvocation(invocation), true, nil
}

func (r *memoryInvocationRepository) attemptsFor(invocationID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.attempts[invocationID]...)
}

func (r *memoryInvocationRepository) replaceInput(invocationID, input string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.invocations[invocationID]
	invocation.InputJSON = input
	sum := sha256.Sum256([]byte(input))
	invocation.InputDigest = fmt.Sprintf("%x", sum[:])
}

func (r *memoryInvocationRepository) replaceInputDigest(invocationID, digest string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invocations[invocationID].InputDigest = digest
}

func (r *memoryInvocationRepository) Complete(_ context.Context, invocationID, attemptID, output string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.invocations[invocationID]
	if invocation == nil || invocation.Status != entity.InvocationRunning || r.attemptIDs[invocationID] != attemptID {
		return false, nil
	}
	invocation.Status = entity.InvocationSucceeded
	invocation.OutputJSON = output
	return true, nil
}

func (r *memoryInvocationRepository) Fail(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finish(invocationID, attemptID, entity.InvocationFailed, reason)
}

func (r *memoryInvocationRepository) MarkUnknown(_ context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finish(invocationID, attemptID, entity.InvocationUnknown, reason)
}

func (r *memoryInvocationRepository) finish(invocationID, attemptID string, status entity.InvocationStatus, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.invocations[invocationID]
	if invocation == nil || invocation.Status != entity.InvocationRunning || r.attemptIDs[invocationID] != attemptID {
		return false, nil
	}
	invocation.Status = status
	invocation.Error = reason
	return true, nil
}

func cloneInvocation(invocation *entity.RuntimeInvocation) *entity.RuntimeInvocation {
	if invocation == nil {
		return nil
	}
	copy := *invocation
	return &copy
}
