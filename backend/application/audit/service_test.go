package audit_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memAuditRepo struct {
	events []*entity.AuditEvent
}

func (m *memAuditRepo) Record(ctx context.Context, e *entity.AuditEvent) error {
	m.events = append(m.events, e)
	return nil
}

func (m *memAuditRepo) List(ctx context.Context, limit, offset int) ([]*entity.AuditEvent, int64, error) {
	total := int64(len(m.events))
	out := make([]*entity.AuditEvent, 0, len(m.events))
	for _, e := range m.events {
		out = append(out, e)
	}
	return out, total, nil
}

func TestService_RecordAndList(t *testing.T) {
	repo := &memAuditRepo{}
	svc := audit.NewService(repo)
	ctx := context.Background()
	if err := svc.Record(ctx, &entity.AuditEvent{Action: "PUT", Resource: "/admin/prompts/x", UserID: 1, Username: "admin", Role: "admin", Status: 200}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := svc.Record(ctx, &entity.AuditEvent{Action: "login", Resource: "oidc", Username: "alice"}); err != nil {
		t.Fatalf("record login: %v", err)
	}
	events, total, err := svc.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("total=%d len=%d", total, len(events))
	}
	if events[0].CreatedAt.IsZero() {
		t.Fatal("created_at must be set")
	}
}

func TestService_EmptyRecordSkipped(t *testing.T) {
	repo := &memAuditRepo{}
	svc := audit.NewService(repo)
	ctx := context.Background()
	if err := svc.Record(ctx, &entity.AuditEvent{}); err != nil {
		t.Fatalf("record empty: %v", err)
	}
	if err := svc.Record(ctx, nil); err != nil {
		t.Fatalf("record nil: %v", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("expected no events, got %d", len(repo.events))
	}
}

func TestService_NilRepoNoop(t *testing.T) {
	svc := audit.NewService(nil)
	if err := svc.Record(context.Background(), &entity.AuditEvent{Action: "x", Resource: "y"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if events, total, err := svc.List(context.Background(), 10, 0); err != nil || events != nil || total != 0 {
		t.Fatalf("list = %v %d %v", events, total, err)
	}
}

var _ port.AuditRepository = (*memAuditRepo)(nil)
