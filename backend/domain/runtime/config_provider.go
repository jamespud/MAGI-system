package runtime

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ConfigProvider loads a MagiConfig by code. The in-memory implementation is
// used here; a DB-backed implementation can be wired via crossdomain.
type ConfigProvider interface {
	Get(ctx context.Context, code string) (*entity.MagiConfig, error)
}

// InMemoryConfigProvider holds MagiConfigs keyed by Code.
type InMemoryConfigProvider struct {
	configs map[string]*entity.MagiConfig
}

// NewInMemoryConfigProvider builds a provider from the given configs.
func NewInMemoryConfigProvider(cfgs ...*entity.MagiConfig) *InMemoryConfigProvider {
	m := make(map[string]*entity.MagiConfig, len(cfgs))
	for _, c := range cfgs {
		if c != nil {
			m[c.Code] = c
		}
	}
	return &InMemoryConfigProvider{configs: m}
}

func (p *InMemoryConfigProvider) Get(ctx context.Context, code string) (*entity.MagiConfig, error) {
	c, ok := p.configs[code]
	if !ok {
		return nil, fmt.Errorf("magi config %q not found", code)
	}
	return c, nil
}
