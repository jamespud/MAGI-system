package plugins_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubBindingRepo struct {
	mu sync.Mutex
	b  map[string]*entity.PluginBinding
}

func newStubBindingRepo() *stubBindingRepo {
	return &stubBindingRepo{b: make(map[string]*entity.PluginBinding)}
}

func (s *stubBindingRepo) Create(ctx context.Context, b *entity.PluginBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b[b.ID] = b
	return nil
}
func (s *stubBindingRepo) Get(ctx context.Context, id string) (*entity.PluginBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.b[id]; ok {
		return b, nil
	}
	return nil, errors.New("not found")
}
func (s *stubBindingRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.PluginBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entity.PluginBinding
	for _, b := range s.b {
		if b.UserID == userID {
			out = append(out, b)
		}
	}
	return out, nil
}
func (s *stubBindingRepo) UpdateEnabled(ctx context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.b[id]; ok {
		b.Enabled = enabled
		return nil
	}
	return errors.New("not found")
}
func (s *stubBindingRepo) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.b, id)
	return nil
}

func TestService_BindingsForUserReturnsOnlyEnabled(t *testing.T) {
	repo := newStubBindingRepo()
	svc := plugins.NewService(repo)
	ctx := context.Background()
	on, _ := svc.Create(ctx, 1, 10, 20, false, true)
	off, _ := svc.Create(ctx, 1, 30, 40, false, false)
	_, _ = svc.Create(ctx, 2, 50, 60, false, true)

	bindings, err := svc.BindingsForUser(ctx, 1)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings: %v %d", err, len(bindings))
	}
	if bindings[0].PluginID != on.PluginID || bindings[0].ToolID != on.ToolID {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}
	_ = off
}

func TestService_OwnershipEnforced(t *testing.T) {
	repo := newStubBindingRepo()
	svc := plugins.NewService(repo)
	ctx := context.Background()
	b, _ := svc.Create(ctx, 1, 10, 20, false, true)
	if err := svc.SetEnabled(ctx, 2, b.ID, false); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	if err := svc.Delete(ctx, 2, b.ID); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	if err := svc.SetEnabled(ctx, 1, b.ID, false); err != nil {
		t.Fatalf("owner update: %v", err)
	}
}
