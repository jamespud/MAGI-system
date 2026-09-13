package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

var (
	ErrToolDenied           = errors.New("tool runtime: tool denied")
	ErrToolApprovalRequired = errors.New("tool runtime: approval required")
	ErrToolApprovalRejected = errors.New("tool runtime: approval rejected")
	ErrToolQuotaExceeded    = errors.New("tool runtime: quota exceeded")
	ErrToolArgumentsInvalid = errors.New("tool runtime: arguments invalid")
)

// defaultToolOutputMaxBytes bounds one tool result before it is persisted and
// fed back to the model. It stays well below MySQL TEXT (65535 bytes) so the
// JSON envelope and the agent loop's untrusted-data wrapper cannot overflow the
// column that stores magi_tool_call.result.
const defaultToolOutputMaxBytes = 32 * 1024

// outputTruncationMarker renders the suffix appended to a clipped output. It is
// derived from the limit so the message can never drift from the constant.
func outputTruncationMarker(max int) string {
	return "\n[output truncated: exceeded " + strconv.Itoa(max) + " bytes]"
}

// clipToolOutput shortens an oversized result in place and reports whether it
// changed anything. Structured payloads that do not fit are dropped: the
// persisted envelope must stay bounded too.
func clipToolOutput(in *port.ToolExecutionResult, max int) bool {
	if in == nil || max <= 0 || len(in.Output) <= max {
		return false
	}
	cut := max
	for cut > 0 && !utf8.ValidString(in.Output[:cut]) {
		cut--
	}
	in.Output = in.Output[:cut] + outputTruncationMarker(max)
	if in.Structured != nil {
		if raw, err := json.Marshal(in.Structured); err != nil || len(raw) > max {
			in.Structured = nil
		}
	}
	return true
}

// defaultToolErrorMaxBytes bounds the error text an executor may inject into
// the durable record. magi_tool_call.err is TEXT and the same string is fed
// back to the model and published in the tool-call-failed event payload, but
// unlike a result an error is never validated before it is stored. MCP servers
// control their own error text, so this is untrusted input: a failing tool
// could otherwise replay the MySQL 1406 failure the output clamp was added to
// prevent.
const defaultToolErrorMaxBytes = 4 * 1024

// boundedError keeps the wrapped error chain intact (errors.Is/As still see the
// original sentinel) while rendering a shortened message.
type boundedError struct {
	err  error
	text string
}

func (e *boundedError) Error() string { return e.text }
func (e *boundedError) Unwrap() error { return e.err }

// boundErrorMessage shortens an oversized error message and reports whether it
// changed anything.
func boundErrorMessage(err error, max int) (error, bool) {
	if err == nil || max <= 0 {
		return err, false
	}
	msg := err.Error()
	if len(msg) <= max {
		return err, false
	}
	cut := max
	for cut > 0 && !utf8.ValidString(msg[:cut]) {
		cut--
	}
	return &boundedError{err: err, text: msg[:cut] + outputTruncationMarker(max)}, true
}

func (r *Runtime) clipToolResult(in *port.ToolExecutionResult) {
	if clipToolOutput(in, defaultToolOutputMaxBytes) {
		r.metrics.IncToolOutputClipped()
	}
}

func (r *Runtime) clipToolError(err error) error {
	clipped, changed := boundErrorMessage(err, defaultToolErrorMaxBytes)
	if changed {
		r.metrics.IncToolErrorClipped()
	}
	return clipped
}

type Deps struct {
	Kernel    *execution.Kernel
	Executor  port.ToolExecutorPort
	Validator validation.Validator
	Policy    *toolpolicy.Policy
	Quota     port.ToolQuotaPort
	Metrics   *metrics.Registry
	Redactor  *redact.Redactor
}

// Runtime executes tools through the common governance and durability path.
// Evidence extraction deliberately remains with the MAGI agent loop.
type Runtime struct {
	kernel    *execution.Kernel
	executor  port.ToolExecutorPort
	validator validation.Validator
	policy    *toolpolicy.Policy
	quota     port.ToolQuotaPort
	metrics   *metrics.Registry
	redactor  *redact.Redactor
}

func New(d Deps) (*Runtime, error) {
	if d.Kernel == nil || d.Executor == nil || d.Validator == nil {
		return nil, errors.New("tool runtime: kernel, executor, and validator are required")
	}
	return &Runtime{
		kernel: d.Kernel, executor: d.Executor, validator: d.Validator,
		policy: d.Policy, quota: d.Quota, metrics: d.Metrics, redactor: d.Redactor,
	}, nil
}

func (r *Runtime) Execute(ctx context.Context, req Request) (*Result, error) {
	result := &Result{Arguments: r.redact(req.ArgumentsJSON)}
	if req.Definition.Name == "" {
		r.record(false)
		return result, fmt.Errorf("%w: resolved tool definition is required", ErrToolDenied)
	}
	if req.Permission.ToolName == "" || req.Permission.ToolName != req.Definition.Name {
		r.record(false)
		return result, fmt.Errorf("%w: tool %q is not in the resolved permission context", ErrToolDenied, req.Definition.Name)
	}

	if r.policy != nil && r.policy.RequiresApproval(req.Definition.Name) && !r.policy.Allowed(req.Definition.Name) {
		if req.Approval == nil {
			return result, ErrToolApprovalRequired
		}
		decision, err := req.Approval(ctx)
		if err != nil {
			return result, r.safeError(err)
		}
		result.ApprovedBy = decision.DecidedBy
		if !decision.Approved {
			return result, fmt.Errorf("%w: %s", ErrToolApprovalRejected, r.redact(decision.Reason))
		}
	}

	if r.quota != nil {
		allowed, err := r.quota.Allow(ctx, req.UserID, req.Definition.Name)
		if err != nil {
			safeErr := r.safeError(fmt.Errorf("allow tool quota: %w", err))
			r.record(false)
			return result, safeErr
		}
		if !allowed {
			return result, fmt.Errorf("%w: %s", ErrToolQuotaExceeded, req.Definition.Name)
		}
	}

	validationResult := r.validator.Validate(req.Definition.ArgsSchema, []byte(req.ArgumentsJSON))
	if validationResult != nil && !validationResult.Valid {
		result.Violations = validationResult.Violations
		r.record(false)
		return result, fmt.Errorf("%w: %s", ErrToolArgumentsInvalid, r.redact(validationResult.Error()))
	}
	if req.Lifecycle != nil {
		req.Lifecycle(LifecycleValidated)
	}

	canonicalArguments := canonicalArguments(req.ArgumentsJSON)
	result.IdempotencyKey = execution.ToolIdempotencyKey(req.Identity.InvocationID, req.Definition.Name, canonicalArguments)
	var executed *port.ToolExecutionResult
	kernelResult, kernelErr := r.kernel.Execute(ctx, execution.Request{
		Identity:       req.Identity,
		Kind:           execution.InvocationTool,
		OperationName:  req.Definition.Name,
		Input:          canonicalArguments,
		RetrySafety:    retrySafety(req.Definition.EffectClass),
		IdempotencyKey: result.IdempotencyKey,
		LogicalOrdinal: req.Ordinal,
	}, func(executeCtx context.Context) ([]byte, error) {
		if req.Lifecycle != nil {
			req.Lifecycle(LifecycleStarted)
		}
		var executeErr error
		executed, executeErr = r.executor.Execute(executeCtx, port.ToolExecutionRequest{
			ToolName:       req.Definition.Name,
			ArgumentsJSON:  req.ArgumentsJSON,
			UserID:         req.UserID,
			Binding:        req.Definition.Binding,
			ExpectedSchema: req.ExpectedSchema,
			RunID:          req.Identity.RunID,
			StepID:         req.Identity.StepID,
			InvocationID:   req.Identity.InvocationID,
			AttemptID:      req.Identity.AttemptID,
			IdempotencyKey: result.IdempotencyKey,
		})
		if executeErr != nil {
			// Bound the message before the kernel persists it: the same text
			// lands in magi_tool_call.err, the failure event payload, and the
			// model's next message.
			return nil, r.clipToolError(executeErr)
		}
		if executed == nil {
			return nil, errors.New("tool executor returned nil result")
		}
		// Clamp before the kernel persists the output, so the stored invocation
		// and the value returned to the agent loop stay bounded together.
		r.clipToolResult(executed)
		return encodeToolResult(executed), nil
	})
	if kernelErr != nil {
		r.record(false)
		return result, r.safeError(kernelErr)
	}

	if executed == nil {
		executed = decodeToolResult(kernelResult.Output)
	}
	result.Execution = executed
	result.Output = r.redact(executed.Output)
	result.Cached = kernelResult.Cached
	r.record(true)
	return result, nil
}

func canonicalArguments(arguments string) []byte {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return []byte(arguments)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return []byte(arguments)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return []byte(arguments)
	}
	return canonical
}

type persistedToolResult struct {
	Version    string          `json:"version"`
	Output     string          `json:"output"`
	Structured json.RawMessage `json:"structured,omitempty"`
	SourceURI  string          `json:"source_uri,omitempty"`
}

func encodeToolResult(result *port.ToolExecutionResult) []byte {
	persisted := persistedToolResult{
		Version: "tool-runtime:v1", Output: result.Output, SourceURI: result.SourceURI,
	}
	if result.Structured != nil {
		if structured, err := json.Marshal(result.Structured); err == nil {
			persisted.Structured = structured
		}
	}
	encoded, err := json.Marshal(persisted)
	if err != nil {
		return []byte(result.Output)
	}
	return encoded
}

func decodeToolResult(encoded []byte) *port.ToolExecutionResult {
	var persisted persistedToolResult
	if err := json.Unmarshal(encoded, &persisted); err != nil || persisted.Version != "tool-runtime:v1" {
		return &port.ToolExecutionResult{Output: string(encoded)}
	}
	result := &port.ToolExecutionResult{Output: persisted.Output, SourceURI: persisted.SourceURI}
	if len(persisted.Structured) > 0 {
		_ = json.Unmarshal(persisted.Structured, &result.Structured)
	}
	return result
}

func (r *Runtime) record(ok bool) {
	if r.metrics != nil {
		r.metrics.IncToolCall(ok)
	}
}

func (r *Runtime) redact(value string) string {
	return r.redactor.String(value)
}

func (r *Runtime) safeError(err error) error {
	if err == nil {
		return nil
	}
	return &redactedError{cause: err, message: r.redact(err.Error())}
}

type redactedError struct {
	cause   error
	message string
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.cause }
