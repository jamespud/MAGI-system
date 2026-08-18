package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

// SensorToolName is the built-in external deterministic-check tool.
const SensorToolName = "run_check"

// sensorArgsSchema is the JSON Schema for run_check arguments.
const sensorArgsSchema = `{"type":"object","properties":{"check":{"type":"string"},"input":{"type":"string"}},"required":["check"],"additionalProperties":false}`

// SensorCheck declares one registered external deterministic check (for
// example a linter, compiler, or unit-test command).
type SensorCheck struct {
	Name    string
	Command string
	Args    []string
	Timeout int
}

// SensorToolConfig enables the external sensor tool.
type SensorToolConfig struct {
	Enabled bool
	Checks  []SensorCheck
}

// sensorCmdFunc executes a check command and returns combined output.
type sensorCmdFunc func(ctx context.Context, command string, args []string, stdin string) ([]byte, error)

// SensorToolExecutor runs only checks registered in configuration and feeds
// the output back to the calling agent for self-correction.
type SensorToolExecutor struct {
	checks  map[string]SensorCheck
	timeout time.Duration
	runCmd  sensorCmdFunc
}

// NewSensorToolExecutor validates the registered checks and builds the tool.
func NewSensorToolExecutor(cfg SensorToolConfig, runCmd sensorCmdFunc) (port.ToolExecutorPort, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("run_check tool is not enabled")
	}
	if len(cfg.Checks) == 0 {
		return nil, fmt.Errorf("run_check: at least one check is required")
	}
	checks := make(map[string]SensorCheck, len(cfg.Checks))
	for _, check := range cfg.Checks {
		name := strings.TrimSpace(check.Name)
		command := strings.TrimSpace(check.Command)
		if name == "" || command == "" {
			return nil, fmt.Errorf("run_check: check name and command are required")
		}
		if _, exists := checks[name]; exists {
			return nil, fmt.Errorf("run_check: duplicate check %q", name)
		}
		checks[name] = check
	}
	if runCmd == nil {
		runCmd = func(ctx context.Context, command string, args []string, stdin string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			if stdin != "" {
				cmd.Stdin = strings.NewReader(stdin)
			}
			return cmd.CombinedOutput()
		}
	}
	return &SensorToolExecutor{checks: checks, runCmd: runCmd}, nil
}

func (e *SensorToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Check string `json:"check"`
		Input string `json:"input,omitempty"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("run_check: parse args: %w", err)
	}
	check, ok := e.checks[args.Check]
	if !ok {
		return nil, fmt.Errorf("run_check: check %q is not registered", args.Check)
	}
	timeout := time.Duration(check.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	output, err := e.runCmd(runCtx, check.Command, check.Args, args.Input)
	okResult := err == nil
	if runCtx.Err() == context.DeadlineExceeded {
		output = []byte(fmt.Sprintf("check %q timed out after %s", args.Check, timeout))
		okResult = false
	}
	out := map[string]any{
		"check": args.Check, "ok": okResult,
		"output": strings.TrimSpace(string(output)),
	}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

var _ port.ToolExecutorPort = (*SensorToolExecutor)(nil)
