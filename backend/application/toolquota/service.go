package toolquota

import (
	"context"
	"strconv"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

// Service enforces per-user per-tool rate limits using a shared repository so
// limits hold across replicas. A limit of 0 means unlimited.
type Service struct {
	repo             port.ToolQuotaRepository
	defaultPerMinute int
	tools            map[string]int
}

func NewService(repo port.ToolQuotaRepository, defaultPerMinute int, tools map[string]int) *Service {
	return &Service{repo: repo, defaultPerMinute: defaultPerMinute, tools: tools}
}

func (s *Service) Allow(ctx context.Context, userID string, toolName string) (bool, error) {
	if s.repo == nil {
		return true, nil
	}
	if userID == "" {
		return true, nil
	}
	limit := s.defaultPerMinute
	if l, ok := s.tools[toolName]; ok {
		limit = l
	}
	if limit <= 0 {
		return true, nil
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return false, err
	}
	window := time.Now().Truncate(time.Minute)
	return s.repo.TryConsume(ctx, uid, toolName, window, limit)
}
