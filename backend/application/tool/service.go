package tool

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service is the application-layer service for tool management.
type Service struct {
	toolReg port.ToolRegistryPort
}

// NewService creates a ToolService.
func NewService(toolReg port.ToolRegistryPort) *Service {
	return &Service{toolReg: toolReg}
}

// List returns all tools available for the given bindings.
func (s *Service) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	if s.toolReg == nil {
		return nil, nil
	}
	return s.toolReg.List(ctx, bindings)
}

// Get returns a specific tool by name.
func (s *Service) Get(ctx context.Context, name string) (*port.ToolDefinition, error) {
	if s.toolReg == nil {
		return nil, fmt.Errorf("tool registry not configured")
	}
	defs, err := s.toolReg.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i], nil
		}
	}
	return nil, fmt.Errorf("tool %q not found", name)
}
