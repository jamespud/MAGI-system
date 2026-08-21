package magi

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

// DockerCodeRunnerPolicy configures the optional Docker sandbox runtime.
type DockerCodeRunnerPolicy struct {
	CodeRunnerPolicy
	Image          string
	MemoryMB       int64
	CPUs           string
	Runtime        string // optional container runtime (e.g. "runsc" for gVisor lightweight virtualization)
	DockerTimeout  int
	DefaultTimeout int
}

// dockerRunFunc executes docker with args and returns combined output. It is
// a field so tests can inject a fake without a Docker daemon.
type dockerRunFunc func(ctx context.Context, args ...string) ([]byte, error)

// DockerCodeRunnerAdapter implements port.CodeRunnerPort by running code in a
// throwaway, network-isolated container. Every run is bounded by the shared
// guardrails plus memory/CPU/PID/time limits; the container is removed after
// execution (--rm) and never inherits the host network.
type DockerCodeRunnerAdapter struct {
	policy   CodeRunnerPolicy
	image    string
	memoryMB int64
	cpus     string
	runtime  string
	timeout  time.Duration
	runCmd   dockerRunFunc
}

// NewDockerCodeRunnerAdapter builds the Docker sandbox with the given policy.
// An empty dockerRunFunc defaults to exec.CommandContext on the local docker
// CLI.
func NewDockerCodeRunnerAdapter(p DockerCodeRunnerPolicy, runCmd dockerRunFunc) (*DockerCodeRunnerAdapter, error) {
	if strings.TrimSpace(p.Image) == "" {
		return nil, fmt.Errorf("docker coderunner: image is required")
	}
	timeout := time.Duration(p.DockerTimeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(p.DefaultTimeout) * time.Second
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if p.MemoryMB <= 0 {
		p.MemoryMB = 256
	}
	executor := runCmd
	if executor == nil {
		executor = func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		}
	}
	return &DockerCodeRunnerAdapter{
		policy: p.CodeRunnerPolicy, image: p.Image, memoryMB: p.MemoryMB,
		cpus: p.CPUs, runtime: strings.TrimSpace(p.Runtime), timeout: timeout, runCmd: executor,
	}, nil
}

// Run validates the request and executes it in an isolated container.
func (a *DockerCodeRunnerAdapter) Run(ctx context.Context, lang, code string) (string, error) {
	if err := validateCodeRequest(a.policy, lang, code); err != nil {
		return "", err
	}
	interpreter, ok := a.interpreter(lang)
	if !ok {
		return "", fmt.Errorf("docker coderunner: no interpreter for language %q", lang)
	}
	args := []string{"run", "--rm", "--network", "none", "--pids-limit", "64",
		"--security-opt=no-new-privileges:true", "--read-only",
		"--tmpfs", "/tmp:noexec,nosuid,size=64m"}
	if a.runtime != "" {
		args = append(args, "--runtime", a.runtime)
	}
	if a.memoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", a.memoryMB))
	}
	if a.cpus != "" {
		args = append(args, "--cpus", a.cpus)
	}
	args = append(args, "-i", a.image, interpreter[0], interpreter[1], code)

	runCtx := ctx
	var cancel context.CancelFunc
	if a.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}
	out, err := a.runCmd(runCtx, args...)
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("docker coderunner: timed out after %s", a.timeout)
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker coderunner: %s", truncateText(msg, 400))
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *DockerCodeRunnerAdapter) interpreter(lang string) ([]string, bool) {
	switch strings.ToLower(lang) {
	case "python", "python3":
		return []string{"python3", "-c"}, true
	case "javascript", "js":
		return []string{"node", "-e"}, true
	case "bash", "sh":
		return []string{"bash", "-c"}, true
	default:
		return nil, false
	}
}

func truncateText(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

var _ port.CodeRunnerPort = (*DockerCodeRunnerAdapter)(nil)
