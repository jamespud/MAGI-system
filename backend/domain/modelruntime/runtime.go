// Package modelruntime executes model generations through the durable runtime kernel.
package modelruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
)

var ErrInvalidRequest = errors.New("model runtime: invalid request")

const (
	responseCodecPrefix = "magi:model-response:gob:v1:"
	inputCodecPrefix    = "magi:model-input:gob:v1:"
)

var registerGobTypesOnce sync.Once

type responseEnvelopeV1 struct {
	Version string
	Message *schema.Message
}

type inputEnvelopeV1 struct {
	Version  string
	Model    modelIdentityV1
	Messages []*schema.Message
}

type modelIdentityV1 struct {
	ModelID   int64
	BaseURL   string
	ModelName string
	Params    *modelParamsV1
	Fallbacks []modelIdentityV1
}

type modelParamsV1 struct {
	Temperature      *float32
	MaxTokens        int
	TopP             *float32
	TopK             *int32
	FrequencyPenalty float32
	PresencePenalty  float32
	ResponseFormat   entity.ResponseFormat
	EnableThinking   *bool
}

type Request struct {
	Identity entity.ExecutionIdentity
	ModelRef entity.ModelRef
	Model    model.ToolCallingChatModel
	Input    []*schema.Message
}

type Runtime struct {
	kernel *execution.Kernel
}

func New(kernel *execution.Kernel) *Runtime {
	return &Runtime{kernel: kernel}
}

// NewInvocationID returns the versioned logical identity for ordinal zero of
// a model generation at stepID. API keys and price metadata are excluded from
// the digest because neither changes which provider/model configuration runs.
func NewInvocationID(stepID string, ref entity.ModelRef) string {
	return execution.NewModelInvocationID(stepID, modelDigest(ref), 0)
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
	if req.Identity.InvocationID != NewInvocationID(req.Identity.StepID, req.ModelRef) {
		return nil, fmt.Errorf("%w: model invocation identity does not match model reference", ErrInvalidRequest)
	}
	input, err := encodeInput(req.ModelRef, req.Input)
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
		output, marshalErr := encodeResponse(response)
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

	return decodeResponse(result.Output)
}

func encodeInput(ref entity.ModelRef, messages []*schema.Message) ([]byte, error) {
	return encodeGob(inputCodecPrefix, inputEnvelopeV1{Version: "v1", Model: canonicalModelIdentity(ref), Messages: messages})
}

func encodeResponse(response *schema.Message) ([]byte, error) {
	return encodeGob(responseCodecPrefix, responseEnvelopeV1{Version: "v1", Message: response})
}

func decodeResponse(encoded []byte) (*schema.Message, error) {
	var envelope responseEnvelopeV1
	if err := decodeGob(responseCodecPrefix, encoded, &envelope); err != nil {
		return nil, fmt.Errorf("model runtime: deserialize persisted response: %w", err)
	}
	if envelope.Version != "v1" || envelope.Message == nil {
		return nil, errors.New("model runtime: invalid persisted response envelope")
	}
	return envelope.Message, nil
}

// Gob's wire format carries concrete type definitions. The registry makes the
// supported dynamic values in Eino's Extra maps round-trip without JSON's
// float64 coercion. Unsupported interface values make Encode fail closed.
func encodeGob(prefix string, value any) ([]byte, error) {
	registerGobTypes()
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(value); err != nil {
		return nil, err
	}
	return []byte(prefix + base64.StdEncoding.EncodeToString(buffer.Bytes())), nil
}

func decodeGob(prefix string, encoded []byte, target any) error {
	registerGobTypes()
	serialized := string(encoded)
	if !strings.HasPrefix(serialized, prefix) {
		return errors.New("unsupported persisted value codec")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(serialized, prefix))
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	return gob.NewDecoder(bytes.NewReader(raw)).Decode(target)
}

func registerGobTypes() {
	registerGobTypesOnce.Do(func() {
		gob.Register(map[string]any{})
		gob.Register([]any{})
		gob.Register([]byte{})
		gob.Register([]string{})
		gob.Register(false)
		gob.Register("")
		gob.Register(int(0))
		gob.Register(int8(0))
		gob.Register(int16(0))
		gob.Register(int32(0))
		gob.Register(int64(0))
		gob.Register(uint(0))
		gob.Register(uint8(0))
		gob.Register(uint16(0))
		gob.Register(uint32(0))
		gob.Register(uint64(0))
		gob.Register(float32(0))
		gob.Register(float64(0))
	})
}

func modelDigest(ref entity.ModelRef) string {
	encoded, err := json.Marshal(canonicalModelIdentity(ref))
	if err != nil {
		panic("model runtime: canonical model identity must be serializable")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func canonicalModelIdentity(ref entity.ModelRef) modelIdentityV1 {
	identity := modelIdentityV1{
		ModelID: ref.ModelID, BaseURL: ref.BaseURL, ModelName: ref.ModelName,
		Fallbacks: make([]modelIdentityV1, len(ref.Fallbacks)),
	}
	if ref.Params != nil {
		identity.Params = &modelParamsV1{
			Temperature: ref.Params.Temperature, MaxTokens: ref.Params.MaxTokens,
			TopP: ref.Params.TopP, TopK: ref.Params.TopK,
			FrequencyPenalty: ref.Params.FrequencyPenalty, PresencePenalty: ref.Params.PresencePenalty,
			ResponseFormat: ref.Params.ResponseFormat, EnableThinking: ref.Params.EnableThinking,
		}
	}
	for i := range ref.Fallbacks {
		identity.Fallbacks[i] = canonicalModelIdentity(ref.Fallbacks[i])
	}
	return identity
}
