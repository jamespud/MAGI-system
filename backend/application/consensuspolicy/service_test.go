package consensuspolicy_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/consensuspolicy"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/port"
)

type memPolicyRepo struct {
	policy *consensus.ConsensusPolicy
}

func (m *memPolicyRepo) Get(ctx context.Context) (*consensus.ConsensusPolicy, error) {
	return m.policy, nil
}
func (m *memPolicyRepo) Save(ctx context.Context, p consensus.ConsensusPolicy) error {
	m.policy = &p
	return nil
}

func TestService_GetFallsBackToDefault(t *testing.T) {
	svc := consensuspolicy.NewService(&memPolicyRepo{})
	p, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Quorum != 2 {
		t.Fatalf("default quorum = %d", p.Quorum)
	}
}

func TestService_SaveAndReset(t *testing.T) {
	repo := &memPolicyRepo{}
	svc := consensuspolicy.NewService(repo)
	ctx := context.Background()
	updated, err := svc.Save(ctx, consensus.ConsensusPolicy{Quorum: 3, FirstSplitGoesToDebate: false})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if updated.Quorum != 3 {
		t.Fatalf("saved = %+v", updated)
	}
	if _, err := svc.Save(ctx, consensus.ConsensusPolicy{Quorum: 0}); err == nil {
		t.Fatal("quorum 0 must be rejected")
	}
	reset, err := svc.Reset(ctx)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.Quorum != 2 {
		t.Fatalf("reset = %+v", reset)
	}
}

var _ port.ConsensusPolicyRepository = (*memPolicyRepo)(nil)
