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
	"reflect"
	"sort"
	"strconv"
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
	inputCodecPrefix    = "magi:model-input:canonical-json:v1:"
	legacyInputPrefix   = "magi:model-input:gob:v1:"
)

var registerGobTypesOnce sync.Once

type responseEnvelopeV1 struct {
	Version string
	Message *schema.Message
}

type inputEnvelopeV1 struct {
	Version  string          `json:"version"`
	Model    modelIdentityV1 `json:"model"`
	Messages canonicalValue  `json:"messages"`
}

type legacyInputEnvelopeV1 struct {
	Version  string
	Model    modelIdentityV1
	Messages []*schema.Message
}

type canonicalValue struct {
	Type     string              `json:"type"`
	Nil      bool                `json:"nil,omitempty"`
	Scalar   string              `json:"scalar,omitempty"`
	Fields   []canonicalField    `json:"fields,omitempty"`
	Elements []canonicalValue    `json:"elements,omitempty"`
	Entries  []canonicalMapEntry `json:"entries,omitempty"`
}

type canonicalField struct {
	Name  string         `json:"name"`
	Value canonicalValue `json:"value"`
}

type canonicalMapEntry struct {
	Key   string         `json:"key"`
	Value canonicalValue `json:"value"`
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

	result, err := r.kernel.ExecuteWithInputMatcher(ctx, execution.Request{
		Identity:      req.Identity,
		Kind:          execution.InvocationModel,
		OperationName: "generate",
		Input:         input,
		RetrySafety:   execution.RetryUnsafe,
	}, matchLegacyInput, func(callCtx context.Context) ([]byte, error) {
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
	canonicalMessages, err := canonicalizeInput(reflect.ValueOf(messages))
	if err != nil {
		return nil, err
	}
	return encodeCanonicalInput(canonicalModelIdentity(ref), canonicalMessages)
}

func encodeCanonicalInput(modelIdentity modelIdentityV1, messages canonicalValue) ([]byte, error) {
	encoded, err := json.Marshal(inputEnvelopeV1{
		Version: "v1", Model: modelIdentity, Messages: messages,
	})
	if err != nil {
		return nil, err
	}
	return []byte(inputCodecPrefix + string(encoded)), nil
}

func matchLegacyInput(invocation *entity.RuntimeInvocation, req execution.Request) (bool, error) {
	if !strings.HasPrefix(invocation.InputJSON, legacyInputPrefix) {
		return false, nil
	}
	var legacy legacyInputEnvelopeV1
	if err := decodeGob(legacyInputPrefix, []byte(invocation.InputJSON), &legacy); err != nil {
		return false, err
	}
	if legacy.Version != "v1" {
		return false, errors.New("unsupported legacy model input version")
	}
	canonicalMessages, err := canonicalizeInput(reflect.ValueOf(legacy.Messages))
	if err != nil {
		return false, err
	}
	canonicalInput, err := encodeCanonicalInput(normalizeLegacyModelIdentity(legacy.Model), canonicalMessages)
	if err != nil {
		return false, err
	}
	return bytes.Equal(canonicalInput, req.Input), nil
}

// Gob v1 does not preserve a non-nil empty Fallbacks slice. Normalize only
// that lossy legacy representation before comparing the canonical request.
func normalizeLegacyModelIdentity(identity modelIdentityV1) modelIdentityV1 {
	normalized := identity
	normalized.Fallbacks = make([]modelIdentityV1, len(identity.Fallbacks))
	for i := range identity.Fallbacks {
		normalized.Fallbacks[i] = normalizeLegacyModelIdentity(identity.Fallbacks[i])
	}
	return normalized
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

func canonicalizeInput(value reflect.Value) (canonicalValue, error) {
	return canonicalizeInputPath(value, make(map[canonicalVisit]struct{}))
}

type canonicalVisit struct {
	type_ reflect.Type
	ptr   uintptr
}

func canonicalizeInputPath(value reflect.Value, path map[canonicalVisit]struct{}) (canonicalValue, error) {
	if !value.IsValid() {
		return canonicalValue{Type: "invalid", Nil: true}, nil
	}
	typeName := canonicalTypeName(value.Type())
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return canonicalValue{Type: typeName, Nil: true}, nil
		}
		return canonicalizeInputPath(value.Elem(), path)
	case reflect.Pointer:
		if value.IsNil() {
			return canonicalValue{Type: typeName, Nil: true}, nil
		}
		leave, err := enterCanonicalPath(value, path)
		if err != nil {
			return canonicalValue{}, err
		}
		defer leave()
		element, err := canonicalizeInputPath(value.Elem(), path)
		if err != nil {
			return canonicalValue{}, err
		}
		return canonicalValue{Type: typeName, Elements: []canonicalValue{element}}, nil
	case reflect.Bool:
		return canonicalValue{Type: typeName, Scalar: strconv.FormatBool(value.Bool())}, nil
	case reflect.String:
		return canonicalValue{Type: typeName, Scalar: value.String()}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return canonicalValue{Type: typeName, Scalar: strconv.FormatInt(value.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return canonicalValue{Type: typeName, Scalar: strconv.FormatUint(value.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		return canonicalValue{Type: typeName, Scalar: strconv.FormatFloat(value.Float(), 'x', -1, value.Type().Bits())}, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return canonicalValue{Type: typeName, Nil: true}, nil
		}
		var leave func()
		if value.Kind() == reflect.Slice {
			var err error
			leave, err = enterCanonicalPath(value, path)
			if err != nil {
				return canonicalValue{}, err
			}
			defer leave()
		}
		elements := make([]canonicalValue, value.Len())
		for i := range elements {
			element, err := canonicalizeInputPath(value.Index(i), path)
			if err != nil {
				return canonicalValue{}, err
			}
			elements[i] = element
		}
		return canonicalValue{Type: typeName, Elements: elements}, nil
	case reflect.Map:
		if value.IsNil() {
			return canonicalValue{Type: typeName, Nil: true}, nil
		}
		leave, err := enterCanonicalPath(value, path)
		if err != nil {
			return canonicalValue{}, err
		}
		defer leave()
		if value.Type().Key().Kind() != reflect.String {
			return canonicalValue{}, fmt.Errorf("unsupported model input map key type: %s", value.Type().Key())
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		entries := make([]canonicalMapEntry, len(keys))
		for i, key := range keys {
			entry, err := canonicalizeInputPath(value.MapIndex(key), path)
			if err != nil {
				return canonicalValue{}, err
			}
			entries[i] = canonicalMapEntry{Key: key.String(), Value: entry}
		}
		return canonicalValue{Type: typeName, Entries: entries}, nil
	case reflect.Struct:
		if !isSupportedInputStruct(value.Type()) {
			return canonicalValue{}, fmt.Errorf("unsupported model input struct type: %s", value.Type())
		}
		fields := make([]canonicalField, 0, value.NumField())
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			fieldValue, err := canonicalizeInputPath(value.Field(i), path)
			if err != nil {
				return canonicalValue{}, err
			}
			fields = append(fields, canonicalField{Name: field.Name, Value: fieldValue})
		}
		return canonicalValue{Type: typeName, Fields: fields}, nil
	default:
		return canonicalValue{}, fmt.Errorf("unsupported model input value type: %s", value.Type())
	}
}

func enterCanonicalPath(value reflect.Value, path map[canonicalVisit]struct{}) (func(), error) {
	ptr := value.Pointer()
	if ptr == 0 {
		return func() {}, nil
	}
	visit := canonicalVisit{type_: value.Type(), ptr: ptr}
	if _, found := path[visit]; found {
		return nil, fmt.Errorf("cyclic model input value: %s", value.Type())
	}
	path[visit] = struct{}{}
	return func() { delete(path, visit) }, nil
}

func canonicalTypeName(t reflect.Type) string {
	if t.Name() != "" && t.PkgPath() != "" {
		return t.PkgPath() + "." + t.Name()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + canonicalTypeName(t.Elem())
	case reflect.Slice:
		return "[]" + canonicalTypeName(t.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(t.Len()) + "]" + canonicalTypeName(t.Elem())
	case reflect.Map:
		return "map[" + canonicalTypeName(t.Key()) + "]" + canonicalTypeName(t.Elem())
	}
	if t.PkgPath() != "" {
		return t.PkgPath() + "." + t.Name()
	}
	return t.String()
}

func isSupportedInputStruct(t reflect.Type) bool {
	switch t {
	case reflect.TypeOf(schema.Message{}),
		reflect.TypeOf(schema.ToolCall{}),
		reflect.TypeOf(schema.FunctionCall{}),
		reflect.TypeOf(schema.ChatMessagePart{}),
		reflect.TypeOf(schema.ChatMessageImageURL{}),
		reflect.TypeOf(schema.ChatMessageAudioURL{}),
		reflect.TypeOf(schema.ChatMessageVideoURL{}),
		reflect.TypeOf(schema.ChatMessageFileURL{}),
		reflect.TypeOf(schema.ResponseMeta{}),
		reflect.TypeOf(schema.TokenUsage{}),
		reflect.TypeOf(schema.PromptTokenDetails{}),
		reflect.TypeOf(schema.LogProbs{}),
		reflect.TypeOf(schema.LogProb{}),
		reflect.TypeOf(schema.TopLogProb{}):
		return true
	default:
		return false
	}
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
