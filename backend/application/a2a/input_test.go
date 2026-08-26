package a2aapp

import (
	"errors"
	"strings"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestInputParserParseTextAndMetadata(t *testing.T) {
	parser := InputParser{MaxMessageBytes: 65536, MaxParts: 4}
	got, err := parser.Parse(&a2a.SendMessageRequest{Message: &a2a.Message{
		ID: "msg-1", Role: a2a.MessageRoleUser,
		ContextID: "ctx-1",
		Parts:     a2a.ContentParts{a2a.NewTextPart("first"), a2a.NewTextPart("second")},
		Metadata: map[string]any{"magi": map[string]any{
			"background":  "background",
			"constraints": []any{map[string]any{"key": "budget", "value": "small", "hard": true}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "msg-1" || got.ContextID != "ctx-1" || got.Question != "first\nsecond" {
		t.Fatalf("parsed input = %+v", got)
	}
	if got.Background != "background" || len(got.Constraints) != 1 || got.Constraints[0] != (entity.Constraint{Key: "budget", Value: "small", Hard: true}) {
		t.Fatalf("metadata = %+v", got)
	}
	if got.RequestHash == "" {
		t.Fatal("request hash is empty")
	}
}

func TestInputParserRejectsInvalidMessages(t *testing.T) {
	tests := []struct {
		name string
		req  *a2a.SendMessageRequest
		err  error
	}{
		{"missing message id", &a2a.SendMessageRequest{Message: &a2a.Message{Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}}, ErrInvalidInput},
		{"empty text", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("  ")}}}, ErrInvalidInput},
		{"agent role", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleAgent, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}}, ErrInvalidInput},
		{"task id", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", TaskID: "task", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}}, ErrTaskMessageNotSupported},
		{"raw part", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewRawPart([]byte("x"))}}}, ErrContentTypeNotSupported},
		{"data part", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewDataPart(map[string]any{"x": 1})}}}, ErrContentTypeNotSupported},
		{"push config", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}, Config: &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: "https://example.test"}}}, ErrTaskMessageNotSupported},
		{"output mode", &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}}, Config: &a2a.SendMessageConfig{AcceptedOutputModes: []string{"image/png"}}}, ErrContentTypeNotSupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(tt.req)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestInputParserRejectsLimitsAndUnknownMetadata(t *testing.T) {
	tooMany := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{
		a2a.NewTextPart("1"), a2a.NewTextPart("2"), a2a.NewTextPart("3"), a2a.NewTextPart("4"), a2a.NewTextPart("5"),
	}}}
	if _, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("too many parts error = %v", err)
	}
	tooManyConstraints := make([]any, 0, maxConstraints+1)
	for i := 0; i <= maxConstraints; i++ {
		tooManyConstraints = append(tooManyConstraints, map[string]any{"key": "k", "value": "v", "hard": false})
	}
	constraintReq := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}, Metadata: map[string]any{"magi": map[string]any{"constraints": tooManyConstraints}}}}
	if _, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(constraintReq); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("too many constraints error = %v", err)
	}
	if _, err := (InputParser{MaxMessageBytes: 2, MaxParts: 4}).Parse(&a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("123")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("too many bytes error = %v", err)
	}
	unknown := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}, Metadata: map[string]any{"magi": map[string]any{"unknown": true}}}}
	if _, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(unknown); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown metadata error = %v", err)
	}
	deep := any(map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": true}}}}})
	deepReq := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("x")}, Metadata: map[string]any{"magi": deep}}}
	if _, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(deepReq); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("deep metadata error = %v", err)
	}
}

func TestInputParserHashIgnoresMetadataMapOrder(t *testing.T) {
	makeReq := func(m map[string]any) *a2a.SendMessageRequest {
		return &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m", Role: a2a.MessageRoleUser, ContextID: "ctx", Parts: a2a.ContentParts{a2a.NewTextPart("x")}, Metadata: map[string]any{"magi": m}}}
	}
	a := makeReq(map[string]any{"background": "b", "constraints": []any{map[string]any{"key": "k", "value": "v", "hard": false}}})
	b := makeReq(map[string]any{"constraints": []any{map[string]any{"hard": false, "value": "v", "key": "k"}}, "background": "b"})
	pa, err := (InputParser{MaxMessageBytes: 65536, MaxParts: 4}).Parse(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := (InputParser{MaxMessageBytes: 65536, MaxParts: 4}).Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if pa.RequestHash != pb.RequestHash {
		t.Fatalf("hashes differ: %s != %s", pa.RequestHash, pb.RequestHash)
	}
}

func TestInputParserRejectsOversizedIdentifiersAndCanonicalBody(t *testing.T) {
	longID := strings.Repeat("m", 129)
	longCtx := strings.Repeat("c", 65)
	msg := func(id, ctxID string, background string) *a2a.SendMessageRequest {
		metadata := map[string]any(nil)
		if background != "" {
			metadata = map[string]any{"magi": map[string]any{"background": background}}
		}
		return &a2a.SendMessageRequest{Message: &a2a.Message{
			ID: id, ContextID: ctxID, Role: a2a.MessageRoleUser,
			Parts: a2a.ContentParts{a2a.NewTextPart("x")}, Metadata: metadata,
		}}
	}
	if _, err := (InputParser{MaxMessageBytes: 65536, MaxParts: 4}).Parse(msg(longID, "", "")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized message id error = %v", err)
	}
	if _, err := (InputParser{MaxMessageBytes: 65536, MaxParts: 4}).Parse(msg("m", longCtx, "")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized context id error = %v", err)
	}
	// The question is tiny but the canonical body (background) blows the
	// message budget: oversized metadata must be rejected before hashing.
	if _, err := (InputParser{MaxMessageBytes: 64, MaxParts: 4}).Parse(msg("m", "", strings.Repeat("b", 200))); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized canonical body error = %v", err)
	}
	// Exact boundary identifiers are accepted.
	okID := strings.Repeat("m", 128)
	okCtx := strings.Repeat("c", 64)
	if _, err := (InputParser{MaxMessageBytes: 65536, MaxParts: 4}).Parse(msg(okID, okCtx, "")); err != nil {
		t.Fatalf("boundary identifiers rejected: %v", err)
	}
}
