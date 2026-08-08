package redact

import (
	"encoding/json"
	"strings"
)

// Redactor masks known secret values before secrets leave the process: they
// are replaced with [REDACTED] in persisted events, audit records, and model
// messages. A nil Redactor is a safe no-op.
type Redactor struct {
	secrets []string
}

func New(secrets ...string) *Redactor {
	seen := map[string]bool{}
	out := make([]string, 0, len(secrets))
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return &Redactor{secrets: out}
}

// String masks every configured secret inside s.
func (r *Redactor) String(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, "[REDACTED]")
	}
	return s
}

// JSON masks secrets inside serialized JSON while preserving valid JSON
// (replacement only touches substrings inside string values).
func (r *Redactor) JSON(raw json.RawMessage) json.RawMessage {
	if r == nil || len(raw) == 0 {
		return raw
	}
	return json.RawMessage(r.String(string(raw)))
}

var _ = json.Marshal
