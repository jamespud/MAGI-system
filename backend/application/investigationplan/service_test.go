package investigationplan_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/investigationplan"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memInvestigationPlanRepo struct {
	plans map[string]*entity.InvestigationPlan
}

func (m *memInvestigationPlanRepo) Get(ctx context.Context, caseID string) (*entity.InvestigationPlan, error) {
	if p, ok := m.plans[caseID]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *memInvestigationPlanRepo) Save(ctx context.Context, p *entity.InvestigationPlan) error {
	m.plans[p.CaseID] = p
	return nil
}

func TestService_GetAbsentReturnsNil(t *testing.T) {
	svc := investigationplan.NewService(&memInvestigationPlanRepo{plans: map[string]*entity.InvestigationPlan{}})
	p, err := svc.Get(context.Background(), "case-missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p != nil {
		t.Fatalf("expected nil plan, got %+v", p)
	}
}

func TestService_SaveAndGetRoundtrip(t *testing.T) {
	svc := investigationplan.NewService(&memInvestigationPlanRepo{plans: map[string]*entity.InvestigationPlan{}})
	ctx := context.Background()
	items := []entity.InvestigationPlanItem{
		{Question: "网络可达性与依赖？", Background: "排查 DNS/TCP/TLS"},
		{Question: "数据库连接池是否打满？"},
	}
	got, err := svc.Save(ctx, "case-1", items)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.CaseID != "case-1" || len(got.Items) != 2 || got.Items[0].Question != items[0].Question {
		t.Fatalf("saved = %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated_at must be set")
	}

	loaded, err := svc.Get(ctx, "case-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded == nil || len(loaded.Items) != 2 || loaded.Items[1].Question != "数据库连接池是否打满？" {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestService_SaveValidates(t *testing.T) {
	svc := investigationplan.NewService(&memInvestigationPlanRepo{plans: map[string]*entity.InvestigationPlan{}})
	ctx := context.Background()
	if _, err := svc.Save(ctx, "case-1", nil); err == nil {
		t.Fatal("empty plan must be rejected")
	}
	if _, err := svc.Save(ctx, "case-1", []entity.InvestigationPlanItem{{Question: "  "}}); err == nil {
		t.Fatal("blank question must be rejected")
	}
}

var _ port.InvestigationPlanRepository = (*memInvestigationPlanRepo)(nil)
