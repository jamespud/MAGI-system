package port

import (
	"context"
	"errors"
)

const DecisionWriterContractVersion = 1

var (
	ErrDecisionWriterSchema   = errors.New("decision writer schema incompatible")
	ErrDecisionWriterBlocked  = errors.New("decision writer admission blocked")
	ErrDecisionWriterVersion  = errors.New("decision writer contract version incompatible")
	ErrDecisionWriterIdentity = errors.New("decision writer database identity incompatible")
	ErrExecutionOwnerRequired = errors.New("durable decision execution requires an owner")
	ErrCutoverPrecondition    = errors.New("decision cutover precondition failed")
)

// DecisionWriterGuard is checked before recovery can launch any execution.
// Admit/Claim must additionally check it under a shared gate lock in their own
// transaction; this reader alone is not a linearization boundary.
type DecisionWriterGuard interface {
	CheckDecisionWriter(context.Context) error
}

// ExecutionOwnerPolicy marks production persistence. Pure memory and explicit
// legacy fixtures may retain ownerless compatibility; runtime assembly may not.
type ExecutionOwnerPolicy interface {
	RequiresExecutionOwner() bool
}
