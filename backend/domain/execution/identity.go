package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

type InvocationKind string

const (
	InvocationModel  InvocationKind = "model"
	InvocationTool   InvocationKind = "tool"
	InvocationSensor InvocationKind = "sensor"
)

// NewStepID returns the logical identity of a step within a run.
func NewStepID(runID string, stepIndex int) string {
	return digest("step:v1|" + runID + "|" + strconv.Itoa(stepIndex))
}

// NewInvocationID returns the logical identity of an invocation within a step.
func NewInvocationID(stepID string, kind InvocationKind, ordinal int) string {
	return digest("invocation:v1|" + stepID + "|" + string(kind) + "|" + strconv.Itoa(ordinal))
}

// NewModelInvocationID scopes a model invocation to the immutable selected
// model while leaving the generic invocation identity contract unchanged.
func NewModelInvocationID(stepID, modelDigest string, ordinal int) string {
	return digest("model-invocation:v1|" + stepID + "|" + modelDigest + "|" + strconv.Itoa(ordinal))
}

// ToolIdempotencyKey returns the stable idempotency key for a tool invocation.
func ToolIdempotencyKey(invocationID string, toolName string, canonicalArguments []byte) string {
	argumentsHash := digestBytes(canonicalArguments)
	return digest("tool:v1|" + invocationID + "|" + toolName + "|" + argumentsHash)
}

// ApprovalIntentDigest binds a human approval decision to one exact invocation
// intent: the tool plus its canonical arguments. It deliberately shares the
// canonical argument encoding with ToolIdempotencyKey so "the same call" means
// the same thing to the approval key and to the execution kernel. A call that
// changes any business argument produces a different digest and therefore
// cannot inherit an earlier decision.
func ApprovalIntentDigest(toolName string, canonicalArguments []byte) string {
	argumentsHash := digestBytes(canonicalArguments)
	return digest("approval-intent:v1|" + toolName + "|" + argumentsHash)
}

func digest(value string) string {
	return digestBytes([]byte(value))
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
