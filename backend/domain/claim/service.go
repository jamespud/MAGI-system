package claim

import (
	"context"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Service manages claims and the claim graph.
type Service struct {
	mu     sync.Mutex
	claims map[string]*entity.Claim
	order  []string
}

func NewService() *Service {
	return &Service{claims: make(map[string]*entity.Claim)}
}

func (s *Service) Create(ctx context.Context, c *entity.Claim) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims[c.ID] = c
	s.order = append(s.order, c.ID)
}

func (s *Service) Get(ctx context.Context, id string) (*entity.Claim, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.claims[id]
	return c, ok
}

func (s *Service) List(ctx context.Context) []*entity.Claim {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.Claim, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.claims[id])
	}
	return out
}

func (s *Service) UpdateStatus(ctx context.Context, id string, status entity.ClaimStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.claims[id]; ok {
		c.Status = status
	}
}
