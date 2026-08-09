package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type fakeRepo struct {
	mu   sync.Mutex
	reqs map[string]*entity.ApprovalRequest
}

func newFakeRepo() *fakeRepo { return &fakeRepo{reqs: map[string]*entity.ApprovalRequest{}} }

func (r *fakeRepo) Create(_ context.Context, a *entity.ApprovalRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs[a.ID] = a
	return nil
}
func (r *fakeRepo) Get(_ context.Context, id string) (*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return a, nil
}
func (r *fakeRepo) FindByKey(_ context.Context, caseID, runID, toolName string) (*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.reqs {
		if a.CaseID == caseID && a.RunID == runID && a.ToolName == toolName {
			return a, nil
		}
	}
	return nil, nil
}
func (r *fakeRepo) List(_ context.Context, caseID string) ([]*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*entity.ApprovalRequest
	for _, a := range r.reqs {
		if a.CaseID == caseID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *fakeRepo) ListAll(_ context.Context) ([]*entity.ApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*entity.ApprovalRequest, 0, len(r.reqs))
	for _, a := range r.reqs {
		out = append(out, a)
	}
	return out, nil
}
func (r *fakeRepo) Approve(_ context.Context, id, decidedBy, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok || a.Status != entity.ApprovalPending {
		return errors.New("not pending")
	}
	a.Status = entity.ApprovalApproved
	a.DecidedBy = decidedBy
	a.Reason = reason
	return nil
}
func (r *fakeRepo) Reject(_ context.Context, id, decidedBy, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok || a.Status != entity.ApprovalPending {
		return errors.New("not pending")
	}
	a.Status = entity.ApprovalRejected
	a.DecidedBy = decidedBy
	a.Reason = reason
	return nil
}
func (r *fakeRepo) MarkExpired(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.reqs[id]
	if !ok || a.Status != entity.ApprovalPending {
		return errors.New("not pending")
	}
	a.Status = entity.ApprovalExpired
	return nil
}

func TestService_CreateDefaultsAndDecide(t *testing.T) {
	svc := approval.NewService(newFakeRepo())
	created, err := svc.Create(context.Background(), &entity.ApprovalRequest{
		CaseID: "c1", RunID: "r1", ToolName: "code_runner", Arguments: `{"x":1}`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.Status != entity.ApprovalPending {
		t.Fatalf("defaults missing: %+v", created)
	}
	approved, err := svc.Approve(context.Background(), created.ID, "human-1", "ok")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != entity.ApprovalApproved || approved.DecidedBy != "human-1" {
		t.Fatalf("approved: %+v", approved)
	}
	if _, err := svc.Approve(context.Background(), created.ID, "human-2", "again"); err == nil {
		t.Fatal("double approve should fail")
	}
}

func TestService_RejectAndList(t *testing.T) {
	repo := newFakeRepo()
	svc := approval.NewService(repo)
	a, _ := svc.Create(context.Background(), &entity.ApprovalRequest{CaseID: "c2", RunID: "r2", ToolName: "calc"})
	rejected, err := svc.Reject(context.Background(), a.ID, "human-2", "too risky")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != entity.ApprovalRejected {
		t.Fatalf("rejected: %+v", rejected)
	}
	list, err := svc.List(context.Background(), "c2")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	all, err := svc.List(context.Background(), "")
	if err != nil || len(all) != 1 {
		t.Fatalf("list all: %v len=%d", err, len(all))
	}
}

var _ port.ApprovalRepository = (*fakeRepo)(nil)
