package plugins

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages user-scoped plugin tool bindings and resolves the enabled
// bindings into runtime ToolBindings for agent runs.
type Service struct {
	repo port.PluginBindingRepository
}

func NewService(repo port.PluginBindingRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, userID int64) ([]*entity.PluginBinding, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *Service) Create(ctx context.Context, userID, pluginID, toolID int64, isDraft, enabled bool) (*entity.PluginBinding, error) {
	if userID == 0 {
		return nil, fmt.Errorf("plugins: authenticated user is required")
	}
	if pluginID <= 0 || toolID <= 0 {
		return nil, fmt.Errorf("plugins: plugin_id and tool_id are required")
	}
	b := &entity.PluginBinding{ID: "pb-" + uuid.NewString(), UserID: userID, PluginID: pluginID, ToolID: toolID, IsDraft: isDraft, Enabled: enabled, CreatedAt: time.Now()}
	if err := s.repo.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("plugins: create: %w", err)
	}
	return b, nil
}

func (s *Service) SetEnabled(ctx context.Context, userID int64, id string, enabled bool) error {
	b, err := s.repo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("plugins: get: %w", err)
	}
	if b.UserID != userID {
		return fmt.Errorf("plugins: forbidden")
	}
	return s.repo.UpdateEnabled(ctx, id, enabled)
}

func (s *Service) Delete(ctx context.Context, userID int64, id string) error {
	b, err := s.repo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("plugins: get: %w", err)
	}
	if b.UserID != userID {
		return fmt.Errorf("plugins: forbidden")
	}
	return s.repo.Delete(ctx, id)
}

// BindingsForUser resolves the user's enabled plugin bindings into runtime
// tool bindings. It satisfies the dispatcher's ToolBindingsProvider.
func (s *Service) BindingsForUser(ctx context.Context, userID int64) ([]entity.ToolBinding, error) {
	if userID == 0 {
		return nil, nil
	}
	all, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []entity.ToolBinding
	for _, b := range all {
		if b.Enabled {
			out = append(out, b.ToolBinding())
		}
	}
	return out, nil
}
