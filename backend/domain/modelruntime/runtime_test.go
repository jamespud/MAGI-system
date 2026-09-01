package modelruntime_test

import (
	"context"
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
		Role:    schema.Assistant,
		Content: "response A",
		ToolCalls: []schema.ToolCall{{
			ID:   "call-a",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "search",
				Arguments: `{"query":"MAGI"}`,
			},
		}},
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: "tool_calls",
			Usage:        &schema.TokenUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		},
	}
	secondResponse := schema.AssistantMessage("response B", nil)
	provider := &scriptedModel{responses: []*schema.Message{firstResponse, secondResponse}}
	stepID := execution.NewStepID("run-1", 1)
	messages := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}

	first, err := runtime.Generate(context.Background(), modelruntime.Request{
		Identity: entity.ExecutionIdentity{
			RunID: "run-1", StepID: stepID,
			InvocationID: execution.NewInvocationID(stepID, execution.InvocationModel, 0), AttemptID: "attempt-1",
		},
		Model: provider,
		Input: messages,
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
			InvocationID: execution.NewInvocationID(stepID, execution.InvocationModel, 0), AttemptID: "attempt-2",
		},
		Model: provider,
		Input: messages,
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

type memoryInvocationRepository struct {
	mu         sync.Mutex
	invocation *entity.RuntimeInvocation
	attemptID  string
}

func (r *memoryInvocationRepository) Ensure(_ context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation == nil {
		copy := *invocation
		r.invocation = &copy
	}
	return cloneInvocation(r.invocation), nil
}

func (r *memoryInvocationRepository) Get(_ context.Context, _ string) (*entity.RuntimeInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneInvocation(r.invocation), nil
}

func (r *memoryInvocationRepository) BeginAttempt(_ context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation.InvocationID != invocationID || r.invocation.Status == entity.InvocationSucceeded {
		return cloneInvocation(r.invocation), false, nil
	}
	r.invocation.Status = entity.InvocationRunning
	r.attemptID = attemptID
	return cloneInvocation(r.invocation), true, nil
}

func (r *memoryInvocationRepository) Complete(_ context.Context, invocationID, attemptID, output string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invocation.InvocationID != invocationID || r.invocation.Status != entity.InvocationRunning || r.attemptID != attemptID {
		return false, nil
	}
	r.invocation.Status = entity.InvocationSucceeded
	r.invocation.OutputJSON = output
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
	if r.invocation.InvocationID != invocationID || r.invocation.Status != entity.InvocationRunning || r.attemptID != attemptID {
		return false, nil
	}
	r.invocation.Status = status
	r.invocation.Error = reason
	return true, nil
}

func cloneInvocation(invocation *entity.RuntimeInvocation) *entity.RuntimeInvocation {
	if invocation == nil {
		return nil
	}
	copy := *invocation
	return &copy
}
