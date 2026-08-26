package a2aapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jamespud/magi/backend/domain/entity"
)

const (
	maxConstraints   = 32
	maxMetadataDepth = 4
)

var (
	ErrInvalidInput            = errors.New("invalid a2a input")
	ErrContentTypeNotSupported = errors.New("a2a content type not supported")
	ErrTaskMessageNotSupported = errors.New("a2a task message not supported")
)

// InputParser validates the intentionally narrow inbound A2A surface. Zero
// limits use the server defaults so tests and callers can construct it plainly.
type InputParser struct {
	MaxMessageBytes int
	MaxParts        int
}

func NewInputParser(maxMessageBytes, maxParts int) InputParser {
	return InputParser{MaxMessageBytes: maxMessageBytes, MaxParts: maxParts}
}

// ParsedInput is the normalized request data used by the submission service.
type ParsedInput struct {
	MessageID   string
	ContextID   string
	Question    string
	Background  string
	Constraints []entity.Constraint
	RequestHash string
}

type inputMetadata struct {
	Background  string              `json:"background"`
	Constraints []entity.Constraint `json:"constraints"`
}

type canonicalInput struct {
	Question    string              `json:"question"`
	ContextID   string              `json:"contextId,omitempty"`
	Background  string              `json:"background,omitempty"`
	Constraints []entity.Constraint `json:"constraints,omitempty"`
}

func (p InputParser) Parse(req *a2a.SendMessageRequest) (ParsedInput, error) {
	if req == nil || req.Message == nil {
		return ParsedInput{}, fmt.Errorf("%w: message is required", ErrInvalidInput)
	}
	m := req.Message
	if strings.TrimSpace(m.ID) == "" {
		return ParsedInput{}, fmt.Errorf("%w: message id is required", ErrInvalidInput)
	}
	if len([]byte(m.ID)) > 128 {
		return ParsedInput{}, fmt.Errorf("%w: message id exceeds 128 bytes", ErrInvalidInput)
	}
	if len([]byte(m.ContextID)) > 64 {
		return ParsedInput{}, fmt.Errorf("%w: context id exceeds 64 bytes", ErrInvalidInput)
	}
	if m.Role != a2a.MessageRoleUser {
		return ParsedInput{}, fmt.Errorf("%w: role must be ROLE_USER", ErrInvalidInput)
	}
	if m.TaskID != "" {
		return ParsedInput{}, fmt.Errorf("%w: task messages are not supported", ErrTaskMessageNotSupported)
	}
	if req.Config != nil {
		if req.Config.PushConfig != nil {
			return ParsedInput{}, fmt.Errorf("%w: push notifications are not supported", ErrTaskMessageNotSupported)
		}
		for _, mode := range req.Config.AcceptedOutputModes {
			if mode != "text/markdown" && mode != "application/json" {
				return ParsedInput{}, fmt.Errorf("%w: output mode %q", ErrContentTypeNotSupported, mode)
			}
		}
	}
	maxParts := p.MaxParts
	if maxParts <= 0 {
		maxParts = 16
	}
	if len(m.Parts) == 0 || len(m.Parts) > maxParts {
		return ParsedInput{}, fmt.Errorf("%w: parts must be between 1 and %d", ErrInvalidInput, maxParts)
	}
	texts := make([]string, 0, len(m.Parts))
	for _, part := range m.Parts {
		if part == nil {
			return ParsedInput{}, fmt.Errorf("%w: nil part", ErrInvalidInput)
		}
		if part.Text() == "" && (part.Raw() != nil || part.Data() != nil || part.URL() != "") {
			return ParsedInput{}, fmt.Errorf("%w: only text parts are supported", ErrContentTypeNotSupported)
		}
		if part.Text() == "" {
			return ParsedInput{}, fmt.Errorf("%w: empty text part", ErrInvalidInput)
		}
		texts = append(texts, part.Text())
	}
	question := strings.Join(texts, "\n")
	if strings.TrimSpace(question) == "" {
		return ParsedInput{}, fmt.Errorf("%w: question is empty", ErrInvalidInput)
	}
	maxBytes := p.MaxMessageBytes
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	if len([]byte(question)) > maxBytes {
		return ParsedInput{}, fmt.Errorf("%w: message exceeds %d bytes", ErrInvalidInput, maxBytes)
	}

	metadata, err := parseInputMetadata(m.Metadata)
	if err != nil {
		return ParsedInput{}, err
	}
	normalized := canonicalInput{
		Question: question, ContextID: m.ContextID,
		Background: metadata.Background, Constraints: metadata.Constraints,
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return ParsedInput{}, fmt.Errorf("%w: canonical request: %v", ErrInvalidInput, err)
	}
	if len(canonical) > maxBytes {
		return ParsedInput{}, fmt.Errorf("%w: canonical message exceeds %d bytes", ErrInvalidInput, maxBytes)
	}
	hash := sha256.Sum256(canonical)
	return ParsedInput{
		MessageID: m.ID, ContextID: m.ContextID, Question: question,
		Background: metadata.Background, Constraints: metadata.Constraints,
		RequestHash: hex.EncodeToString(hash[:]),
	}, nil
}

func parseInputMetadata(metadata map[string]any) (inputMetadata, error) {
	value, ok := metadata["magi"]
	if !ok || value == nil {
		return inputMetadata{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return inputMetadata{}, fmt.Errorf("%w: magi metadata: %v", ErrInvalidInput, err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil || jsonDepth(generic, 1) > maxMetadataDepth {
		return inputMetadata{}, fmt.Errorf("%w: metadata nesting exceeds %d objects", ErrInvalidInput, maxMetadataDepth)
	}
	var decoded inputMetadata
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return inputMetadata{}, fmt.Errorf("%w: magi metadata: %v", ErrInvalidInput, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return inputMetadata{}, fmt.Errorf("%w: magi metadata has trailing data", ErrInvalidInput)
	}
	if len(decoded.Constraints) > maxConstraints {
		return inputMetadata{}, fmt.Errorf("%w: at most %d constraints are allowed", ErrInvalidInput, maxConstraints)
	}
	return decoded, nil
}

func jsonDepth(value any, objects int) int {
	switch v := value.(type) {
	case map[string]any:
		max := objects
		for _, child := range v {
			if depth := jsonDepth(child, objects+1); depth > max {
				max = depth
			}
		}
		return max
	case []any:
		max := objects
		for _, child := range v {
			if depth := jsonDepth(child, objects); depth > max {
				max = depth
			}
		}
		return max
	default:
		return objects
	}
}
