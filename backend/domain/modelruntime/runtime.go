// Package modelruntime executes model generations through the durable runtime kernel.
package modelruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
)

var ErrInvalidRequest = errors.New("model runtime: invalid request")

type Request struct {
	Identity entity.ExecutionIdentity
	Model    model.ToolCallingChatModel
	Input    []*schema.Message
}

type Runtime struct {
	kernel *execution.Kernel
}

func New(kernel *execution.Kernel) *Runtime {
	return &Runtime{kernel: kernel}
}

// Generate persists the complete Eino response before returning it. Model calls
// are RetryUnsafe because regenerating a response after an ambiguous outcome can
// change the subsequent tool-call sequence.
func (r *Runtime) Generate(ctx context.Context, req Request) (*schema.Message, error) {
	if r == nil || r.kernel == nil {
		return nil, fmt.Errorf("%w: execution kernel is required", ErrInvalidRequest)
	}
	if req.Model == nil {
		return nil, fmt.Errorf("%w: model is required", ErrInvalidRequest)
	}
	input, err := json.Marshal(req.Input)
	if err != nil {
		return nil, fmt.Errorf("%w: serialize model input: %v", ErrInvalidRequest, err)
	}

	result, err := r.kernel.Execute(ctx, execution.Request{
		Identity:      req.Identity,
		Kind:          execution.InvocationModel,
		OperationName: "generate",
		Input:         input,
		RetrySafety:   execution.RetryUnsafe,
	}, func(callCtx context.Context) ([]byte, error) {
		response, generateErr := req.Model.Generate(callCtx, req.Input)
		if generateErr != nil {
			return nil, generateErr
		}
		if response == nil {
			return nil, fmt.Errorf("%w: model returned nil response", execution.ErrExternalOutcomeUnknown)
		}
		output, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			// The provider has already returned a response, but it cannot be made
			// durable. Do not permit a later attempt to generate a different one.
			return nil, fmt.Errorf("%w: serialize model response: %v", execution.ErrExternalOutcomeUnknown, marshalErr)
		}
		return output, nil
	})
	if err != nil {
		return nil, err
	}

	var response schema.Message
	if err := json.Unmarshal(result.Output, &response); err != nil {
		return nil, fmt.Errorf("model runtime: deserialize persisted response: %w", err)
	}
	return &response, nil
}
