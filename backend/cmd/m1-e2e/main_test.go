package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/internal/m1e2e"
)

func TestRunCLISyntheticEmitsBlockedEvidenceWithoutPass(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runCLI([]string{"-mode", "synthetic"}, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var result m1e2e.RunResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if result.Mode != m1e2e.ModeSynthetic || result.M1Verdict != m1e2e.VerdictNotEvaluated {
		t.Fatalf("result = %+v", result)
	}
	if result.Status == m1e2e.StatusPassM1 || result.Status != m1e2e.StatusBlocked {
		t.Fatalf("status = %q, want BLOCKED and never PASS_M1", result.Status)
	}
	if len(result.Blockers()) == 0 || !strings.Contains(strings.Join(result.Blockers(), ","), "CAPABILITY_UNAVAILABLE") {
		t.Fatalf("blockers = %v", result.Blockers())
	}
}

func TestRunCLIStrictFailsWhileBlocked(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"-mode", "real", "-strict"}, &stdout, &stderr)
	if !errors.Is(err, ErrEvidenceNotReady) {
		t.Fatalf("error = %v, want ErrEvidenceNotReady", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("strict mode did not emit evidence JSON before failing")
	}
}

func TestRunCLIRejectsInvalidMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runCLI([]string{"-mode", "invalid"}, &stdout, &stderr); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
