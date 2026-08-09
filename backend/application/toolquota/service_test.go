package toolquota_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/toolquota"
	"github.com/jamespud/magi/backend/domain/port"
)

type fakeQuotaRepo struct {
	allowed bool
	limit   int
}

func (r *fakeQuotaRepo) TryConsume(ctx context.Context, userID int64, toolName string, windowStart time.Time, limit int) (bool, error) {
	r.limit = limit
	return r.allowed, nil
}

func TestService_Allow(t *testing.T) {
	repo := &fakeQuotaRepo{allowed: true}
	svc := toolquota.NewService(repo, 1, map[string]int{"code_runner": 2})
	ok, err := svc.Allow(context.Background(), "7", "code_runner")
	if err != nil || !ok {
		t.Fatalf("allow: ok=%v err=%v", ok, err)
	}
	if repo.limit != 2 {
		t.Fatalf("per-tool limit not used: %d", repo.limit)
	}
	if ok, _ := svc.Allow(context.Background(), "7", "web_search"); !ok {
		t.Fatal("unlisted tool should use default")
	}
	if ok, _ := svc.Allow(context.Background(), "", "code_runner"); !ok {
		t.Fatal("anonymous user should be allowed")
	}
}

var _ port.ToolQuotaRepository = (*fakeQuotaRepo)(nil)
