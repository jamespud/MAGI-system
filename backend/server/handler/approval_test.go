package handler_test

import (
	"context"
	"strings"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/handler"
)

type memApprovalRepo struct{ items []*entity.ApprovalRequest }

func (r *memApprovalRepo) Create(ctx context.Context, a *entity.ApprovalRequest) error {
	r.items = append(r.items, a)
	return nil
}
func (r *memApprovalRepo) Get(ctx context.Context, id string) (*entity.ApprovalRequest, error) {
	for _, a := range r.items {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}
func (r *memApprovalRepo) FindByKey(ctx context.Context, caseID, runID, toolName string) (*entity.ApprovalRequest, error) {
	return nil, nil
}
func (r *memApprovalRepo) List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error) {
	var out []*entity.ApprovalRequest
	for _, a := range r.items {
		if a.CaseID == caseID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *memApprovalRepo) ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error) {
	out := make([]*entity.ApprovalRequest, len(r.items))
	copy(out, r.items)
	return out, nil
}
func (r *memApprovalRepo) Approve(ctx context.Context, id, decidedBy, reason string) error {
	return nil
}
func (r *memApprovalRepo) Reject(ctx context.Context, id, decidedBy, reason string) error { return nil }
func (r *memApprovalRepo) MarkExpired(ctx context.Context, id string) error               { return nil }

var _ port.ApprovalRepository = (*memApprovalRepo)(nil)

// TestApprovalList_CaseIDNeverBypassesOwnership verifies the multi-tenant leak
// fix: filtering by case_id must not skip the per-approval ownership check.
func TestApprovalList_CaseIDNeverBypassesOwnership(t *testing.T) {
	apprRepo := &memApprovalRepo{items: []*entity.ApprovalRequest{
		{ID: "appr-a", CaseID: "case-a", ToolName: "code_runner", Status: entity.ApprovalPending},
		{ID: "appr-b", CaseID: "case-b", ToolName: "code_runner", Status: entity.ApprovalPending},
	}}
	apprSvc := approval.NewService(apprRepo)
	caseSvc := mapCaseGetter{
		"case-a": {ID: "case-a", UserID: 1},
		"case-b": {ID: "case-b", UserID: 2},
	}
	h := handler.NewApprovalHandler(apprSvc, caseSvc)
	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "user1", Key: "k1", UserID: 1, Role: "user"},
		{Name: "user2", Key: "k2", UserID: 2, Role: "user"},
	})
	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.Use(server.Auth(authSvc))
	r.GET("/approvals", h.List)

	// user1 filtering by user2's case must NOT leak user2's approval
	w := ut.PerformRequest(r.Engine, "GET", "/approvals?case_id=case-b", nil,
		ut.Header{Key: "Authorization", Value: "Bearer k1"})
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "appr-b") {
		t.Fatalf("user1 must NOT see user2's approval via case_id filter: %s", w.Body.String())
	}

	// user1 filtering by own case still works
	w = ut.PerformRequest(r.Engine, "GET", "/approvals?case_id=case-a", nil,
		ut.Header{Key: "Authorization", Value: "Bearer k1"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "appr-a") {
		t.Fatalf("user1 own-case approvals: status=%d body=%s", w.Code, w.Body.String())
	}

	// unfiltered list only returns user1's approvals
	w = ut.PerformRequest(r.Engine, "GET", "/approvals", nil,
		ut.Header{Key: "Authorization", Value: "Bearer k1"})
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "appr-b") || !strings.Contains(w.Body.String(), "appr-a") {
		t.Fatalf("user1 unfiltered list leaked user2's approval: %s", w.Body.String())
	}
}

// mapCaseGetter resolves cases for ownership checks by ID.
type mapCaseGetter map[string]entity.DecisionCase

func (m mapCaseGetter) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	if cs, ok := m[id]; ok {
		return &cs, nil
	}
	return nil, nil
}
