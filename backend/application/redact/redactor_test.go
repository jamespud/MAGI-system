package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/redact"
)

func TestRedactor_StringMasksSecrets(t *testing.T) {
	r := redact.New("sk-secret-1", "tok-secret")
	out := r.String("key=sk-secret-1 token=tok-secret ok")
	if strings.Contains(out, "sk-secret-1") || strings.Contains(out, "tok-secret") {
		t.Fatalf("secrets leaked: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

func TestRedactor_JSONStaysValid(t *testing.T) {
	r := redact.New("sk-secret-1")
	raw := json.RawMessage(`{"api_key":"sk-secret-1","tool":"web_search"}`)
	out := r.JSON(raw)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("redacted JSON invalid: %v (%s)", err, out)
	}
	if m["api_key"] != "[REDACTED]" {
		t.Fatalf("not redacted: %s", out)
	}
	if m["tool"] != "web_search" {
		t.Fatalf("unrelated field changed: %s", out)
	}
}

func TestRedactor_NilSafe(t *testing.T) {
	var r *redact.Redactor
	if got := r.String("keep"); got != "keep" {
		t.Fatalf("nil redactor changed input: %s", got)
	}
}
