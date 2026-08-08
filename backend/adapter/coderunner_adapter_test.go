package magi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coze-dev/coze-studio/backend/infra/coderunner"
	magi "github.com/jamespud/magi/backend/adapter"
)

type stubRunner struct {
	called      bool
	lang        string
	code        string
	hasDeadline bool
}

func (s *stubRunner) Run(ctx context.Context, req *coderunner.RunRequest) (*coderunner.RunResponse, error) {
	s.called = true
	s.lang = string(req.Language)
	s.code = req.Code
	if _, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
	}
	return &coderunner.RunResponse{Result: map[string]any{"ok": true}}, nil
}

func withRunner(t *testing.T, r coderunner.Runner) {
	t.Helper()
	coderunner.SetCodeRunner(r)
	t.Cleanup(func() { coderunner.SetCodeRunner(nil) })
}

func TestCodeRunnerAdapter_RejectsBlockedPatterns(t *testing.T) {
	r := &stubRunner{}
	withRunner(t, r)
	a := magi.NewCodeRunnerAdapter()
	_, err := a.Run(context.Background(), "Python", "import os; os.system('rm -rf /')")
	if err == nil || !strings.Contains(err.Error(), "blocked pattern") {
		t.Fatalf("expected blocked pattern error, got %v", err)
	}
	if r.called {
		t.Fatal("runner must not be called for blocked code")
	}
}

func TestCodeRunnerAdapter_RejectsDisallowedLanguage(t *testing.T) {
	r := &stubRunner{}
	withRunner(t, r)
	a := magi.NewCodeRunnerAdapter()
	if _, err := a.Run(context.Background(), "Bash", "echo hi"); err == nil {
		t.Fatal("expected language error")
	}
}

func TestCodeRunnerAdapter_RejectsOversizedCode(t *testing.T) {
	r := &stubRunner{}
	withRunner(t, r)
	p := magi.DefaultCodeRunnerPolicy()
	p.MaxCodeChars = 8
	a := magi.NewCodeRunnerAdapterWithPolicy(p)
	if _, err := a.Run(context.Background(), "Python", "print(123456789)"); err == nil {
		t.Fatal("expected length error")
	}
}

func TestCodeRunnerAdapter_RunsAllowedCodeWithTimeout(t *testing.T) {
	r := &stubRunner{}
	withRunner(t, r)
	a := magi.NewCodeRunnerAdapter()
	out, err := a.Run(context.Background(), "Python", "print(1)")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !r.called || r.lang != "Python" || r.code != "print(1)" {
		t.Fatalf("runner call: %+v", r)
	}
	if !r.hasDeadline {
		t.Fatal("expected timeout deadline on run context")
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("output: %s", out)
	}
	_ = time.Second
}
