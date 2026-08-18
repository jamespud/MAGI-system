package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/consensus"
)

// ConsensusPolicyRepository persists the editable consensus/voting rules.
type ConsensusPolicyRepository interface {
	Get(ctx context.Context) (*consensus.ConsensusPolicy, error)
	Save(ctx context.Context, p consensus.ConsensusPolicy) error
}
