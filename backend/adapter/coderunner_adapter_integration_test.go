package magi_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coderunnersandbox "github.com/coze-dev/coze-studio/backend/infra/coderunner/impl/sandbox"

	magi "github.com/jamespud/magi/backend/adapter"
)

// realRunner builds a CodeRunnerAdapter backed by the actual Coze Deno+Pyodide
// sandbox (backend/sandbox.py). It is skipped when the runtimes or the vendored
// script are unavailable (they are baked into the MAGI Docker image).
func realRunner(t *testing.T, timeoutSeconds int) *magi.CodeRunnerAdapter {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno not found")
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	backendDir := filepath.Clean(filepath.Join(old, ".."))
	if _, err := os.Stat(filepath.Join(backendDir, "sandbox.py")); err != nil {
		t.Skip("backend/sandbox.py not found (run tests from backend/)")
	}
	_, statErr := os.Stat(filepath.Join(backendDir, "node_modules"))
	hadNodeModules := statErr == nil
	if err := os.Chdir(backendDir); err != nil {
		t.Fatalf("chdir %s: %v", backendDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
		if !hadNodeModules {
			// The sandbox creates a node_modules dir in cwd for pyodide; remove
			// it when the test created it so local runs leave the repo clean.
			_ = os.RemoveAll(filepath.Join(backendDir, "node_modules"))
		}
	})

	warmDenoCache(t)

	p := magi.DefaultCodeRunnerPolicy()
	p.TimeoutSeconds = timeoutSeconds
	runner := coderunnersandbox.NewRunner(&coderunnersandbox.Config{
		TimeoutSeconds: float64(timeoutSeconds),
		MemoryLimitMB:  100,
	})
	return magi.NewCodeRunnerAdapterWithRunner(runner, p)
}

// warmDenoCache fetches jsr:@langchain/pyodide-sandbox into the local Deno
// cache (the MAGI image does the same at build time), so the first sandbox run
// is not spent downloading the package.
func warmDenoCache(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "deno", "run", "-A", "jsr:@langchain/pyodide-sandbox@0.0.4", "-c", "print('ok')")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("warm deno cache: %v: %s", err, out)
	}
}

func TestCodeRunnerAdapter_RealSandbox_RunsPython(t *testing.T) {
	a := realRunner(t, 30)
	// Coze's sandbox wrapper runs `asyncio.run(main(Args(args)))`, so user code
	// must define an async main returning a dict.
	out, err := a.Run(context.Background(), "Python", "async def main(args):\n    return {\"sum\": 1 + 2}\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `"sum":3`) {
		t.Fatalf("output: %s", out)
	}
}

func TestCodeRunnerAdapter_RealSandbox_TimeoutKillsInfiniteLoop(t *testing.T) {
	a := realRunner(t, 3)
	_, err := a.Run(context.Background(), "Python", "def main(args):\n    while True:\n        pass\n")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error: %v", err)
	}
}

func TestCodeRunnerAdapter_RealSandbox_BlockedPatternRejected(t *testing.T) {
	a := realRunner(t, 30)
	_, err := a.Run(context.Background(), "Python", "import os; os.system('rm -rf /')")
	if err == nil || !strings.Contains(err.Error(), "blocked pattern") {
		t.Fatalf("expected blocked pattern error, got %v", err)
	}
}

func TestCodeRunnerAdapter_RealSandbox_DisallowedLanguageRejected(t *testing.T) {
	a := realRunner(t, 30)
	_, err := a.Run(context.Background(), "Bash", "echo hi")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected language error, got %v", err)
	}
}
