package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// RuntimeInvocationRepository owns durable logical invocation state and the
// fencing of its physical attempts. A true bool means the caller owns the
// transition and may continue executing; false means its attempt is stale or
// the logical invocation has already reached a terminal result.
type RuntimeInvocationRepository interface {
	Ensure(
		ctx context.Context,
		invocation *entity.RuntimeInvocation,
	) (*entity.RuntimeInvocation, error)

	Get(
		ctx context.Context,
		invocationID string,
	) (*entity.RuntimeInvocation, error)

	BeginAttempt(
		ctx context.Context,
		invocationID string,
		attemptID string,
	) (*entity.RuntimeInvocation, bool, error)

	Complete(
		ctx context.Context,
		invocationID string,
		attemptID string,
		outputJSON string,
	) (bool, error)

	Fail(
		ctx context.Context,
		invocationID string,
		attemptID string,
		reason string,
	) (bool, error)

	MarkUnknown(
		ctx context.Context,
		invocationID string,
		attemptID string,
		reason string,
	) (bool, error)
}
