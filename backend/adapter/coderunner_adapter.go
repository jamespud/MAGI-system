package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coze-dev/coze-studio/backend/infra/coderunner"
	"github.com/jamespud/magi/backend/domain/port"
)

// CodeRunnerPolicy is the deterministic guardrail configuration for the code
// runner. The code runner executes code written by the agent, so every run is
// bounded by language allow-list, code length, a danger-pattern block-list,
// and a timeout.
type CodeRunnerPolicy struct {
	TimeoutSeconds   int
	MaxCodeChars     int
	AllowedLanguages []string
	BlockedPatterns  []string
}

func DefaultCodeRunnerPolicy() CodeRunnerPolicy {
	return CodeRunnerPolicy{
		TimeoutSeconds:   30,
		MaxCodeChars:     4000,
		AllowedLanguages: []string{"Python"},
		BlockedPatterns:  []string{"os.system", "subprocess", "eval(", "exec(", "shutil.rmtree", "socket"},
	}
}

// CodeRunnerAdapter implements port.CodeRunnerPort via Coze infra/coderunner.
type CodeRunnerAdapter struct {
	policy       CodeRunnerPolicy
	runner       coderunner.Runner // injected sandbox runner (default: Coze global)
	activated    bool
	activateOnce sync.Once
	activateErr  error
}

func NewCodeRunnerAdapter() *CodeRunnerAdapter {
	return NewCodeRunnerAdapterWithPolicy(DefaultCodeRunnerPolicy())
}

func NewCodeRunnerAdapterWithPolicy(p CodeRunnerPolicy) *CodeRunnerAdapter {
	return &CodeRunnerAdapter{policy: p}
}

// NewCodeRunnerAdapterWithRunner injects an explicit sandbox runner (for
// example the Coze Deno/Pyodide sandbox runner assembled from MAGI config).
// The Coze global runner is only consulted when no runner is injected.
func NewCodeRunnerAdapterWithRunner(runner coderunner.Runner, p CodeRunnerPolicy) *CodeRunnerAdapter {
	return &CodeRunnerAdapter{policy: p, runner: runner}
}

func (a *CodeRunnerAdapter) activate(ctx context.Context) error {
	a.activateOnce.Do(func() {
		if a.runner != nil {
			a.activated = true
			return
		}
		r := coderunner.GetCodeRunner()
		if r == nil {
			a.activateErr = fmt.Errorf("coderunner adapter: Coze code runner not initialized")
			return
		}
		a.runner = r
		a.activated = true
	})
	return a.activateErr
}

// Run validates the request against the policy, then executes it in a
// time-bounded context.
func (a *CodeRunnerAdapter) Run(ctx context.Context, lang, code string) (string, error) {
	langOK := false
	for _, l := range a.policy.AllowedLanguages {
		if l == lang {
			langOK = true
			break
		}
	}
	if !langOK {
		return "", fmt.Errorf("coderunner: language %q not allowed", lang)
	}
	if len(code) > a.policy.MaxCodeChars {
		return "", fmt.Errorf("coderunner: code exceeds %d chars", a.policy.MaxCodeChars)
	}
	lower := strings.ToLower(code)
	for _, p := range a.policy.BlockedPatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return "", fmt.Errorf("coderunner: blocked pattern %q", p)
		}
	}
	if err := a.activate(ctx); err != nil {
		return "", fmt.Errorf("coderunner adapter: Coze code runner unavailable: %w", err)
	}
	timeout := time.Duration(a.policy.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	resp, err := a.runner.Run(runCtx, &coderunner.RunRequest{
		Language: coderunner.Language(lang),
		Code:     code,
	})
	if err != nil {
		return "", fmt.Errorf("coderunner: run: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("coderunner: empty response")
	}
	out, err := json.Marshal(resp.Result)
	if err != nil {
		return "", fmt.Errorf("coderunner: encode result: %w", err)
	}
	return string(out), nil
}

var _ port.CodeRunnerPort = (*CodeRunnerAdapter)(nil)
