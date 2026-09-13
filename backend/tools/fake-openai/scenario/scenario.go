// Package scenario holds the fake model's response fixtures and its controllable
// modes. Fixtures are embedded, so the built binary is self-contained and the
// fixture-validation test reads exactly the same bytes the server serves.
package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "embed"
)

//go:embed fixtures/normal.json
var defaultFixtures []byte

//go:embed modes.json
var defaultModes []byte

// Modifier describes how a mode deviates from a normal response.
type Modifier struct {
	DelayMS   int  `json:"delay_ms,omitempty"`
	Status    int  `json:"status,omitempty"`
	Malformed bool `json:"malformed,omitempty"`
}

// Store serves fixtures for the active mode. It is safe for concurrent use.
type Store struct {
	mu        sync.Mutex
	responses map[string]json.RawMessage
	modes     map[string]Modifier
	mode      string
}

// New builds a store from embedded fixtures and modes.
func New() (*Store, error) {
	var fixtures struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(defaultFixtures, &fixtures); err != nil {
		return nil, fmt.Errorf("scenario: parse fixtures: %w", err)
	}
	var modes map[string]Modifier
	if err := json.Unmarshal(defaultModes, &modes); err != nil {
		return nil, fmt.Errorf("scenario: parse modes: %w", err)
	}
	if len(fixtures.Responses) == 0 {
		return nil, fmt.Errorf("scenario: no fixtures embedded")
	}
	if _, ok := modes["normal"]; !ok {
		return nil, fmt.Errorf("scenario: modes must define \"normal\"")
	}
	return &Store{responses: fixtures.Responses, modes: modes, mode: "normal"}, nil
}

// FixtureKeys returns the embedded fixture keys (used by the validation test).
func FixtureKeys() ([]string, error) {
	var fixtures struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(defaultFixtures, &fixtures); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(fixtures.Responses))
	for k := range fixtures.Responses {
		out = append(out, k)
	}
	return out, nil
}

// Fixture returns one embedded fixture by key.
func Fixture(key string) (json.RawMessage, bool) {
	var fixtures struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(defaultFixtures, &fixtures); err != nil {
		return nil, false
	}
	raw, ok := fixtures.Responses[key]
	return raw, ok
}

// Response resolves a fixture for a kind (and role, when role-scoped), trying
// "<kind>.<role>" first and then "<kind>".
func (s *Store) Response(kind, role string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role != "" {
		if raw, ok := s.responses[kind+"."+role]; ok {
			return raw, nil
		}
	}
	if raw, ok := s.responses[kind]; ok {
		return raw, nil
	}
	return nil, fmt.Errorf("scenario: no fixture for kind %q role %q", kind, role)
}

// SetMode switches the active mode, optionally overriding the delay.
func (s *Store) SetMode(name string, delayOverrideMS *int) error {
	name = strings.TrimSpace(strings.ToLower(name))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.modes[name]; !ok {
		return fmt.Errorf("scenario: unknown mode %q", name)
	}
	s.mode = name
	if delayOverrideMS != nil {
		m := s.modes[name]
		m.DelayMS = *delayOverrideMS
		s.modes[name] = m
	}
	return nil
}

// Mode reports the active mode and its modifier.
func (s *Store) Mode() (string, Modifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode, s.modes[s.mode]
}
