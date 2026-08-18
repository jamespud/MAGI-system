package consensuspolicy

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages the editable consensus/voting rules used by the
// deterministic consensus engine.
type Service struct {
	repo port.ConsensusPolicyRepository
}

func NewService(repo port.ConsensusPolicyRepository) *Service {
	return &Service{repo: repo}
}

// Get returns the stored rules or the built-in defaults.
func (s *Service) Get(ctx context.Context) (consensus.ConsensusPolicy, error) {
	stored, err := s.repo.Get(ctx)
	if err != nil {
		return consensus.ConsensusPolicy{}, err
	}
	if stored != nil {
		return *stored, nil
	}
	return consensus.DefaultConsensusPolicy(), nil
}

// Save validates and persists the consensus rules.
func (s *Service) Save(ctx context.Context, p consensus.ConsensusPolicy) (consensus.ConsensusPolicy, error) {
	if p.Quorum < 1 {
		return consensus.ConsensusPolicy{}, fmt.Errorf("consensus policy: quorum must be at least 1")
	}
	if err := s.repo.Save(ctx, p); err != nil {
		return consensus.ConsensusPolicy{}, err
	}
	return p, nil
}

// Reset restores the built-in defaults.
func (s *Service) Reset(ctx context.Context) (consensus.ConsensusPolicy, error) {
	def := consensus.DefaultConsensusPolicy()
	if err := s.repo.Save(ctx, def); err != nil {
		return consensus.ConsensusPolicy{}, err
	}
	return def, nil
}
