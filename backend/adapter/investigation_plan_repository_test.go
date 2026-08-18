package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestInvestigationPlanRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.InvestigationPlanModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewInvestigationPlanRepository(db)
	ctx := context.Background()

	if got, _ := repo.Get(ctx, "case-1"); got != nil {
		t.Fatalf("expected no stored plan, got %+v", got)
	}

	items := []entity.InvestigationPlanItem{
		{Question: "网络可达性与依赖？", Background: "排查 DNS/TCP/TLS"},
		{Question: "数据库连接池是否打满？"},
	}
	plan := &entity.InvestigationPlan{CaseID: "case-1", Items: items}
	if err := repo.Save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.Get(ctx, "case-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || len(got.Items) != 2 || got.Items[0].Question != items[0].Question || got.Items[0].Background != "排查 DNS/TCP/TLS" {
		t.Fatalf("roundtrip = %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated_at must be set")
	}

	// Overwrite replaces the previous items.
	replacement := &entity.InvestigationPlan{CaseID: "case-1", Items: []entity.InvestigationPlanItem{{Question: "只查一个问题"}}}
	if err := repo.Save(ctx, replacement); err != nil {
		t.Fatalf("resave: %v", err)
	}
	got2, _ := repo.Get(ctx, "case-1")
	if len(got2.Items) != 1 || got2.Items[0].Question != "只查一个问题" {
		t.Fatalf("overwrite = %+v", got2)
	}

	// Other cases stay empty.
	other, _ := repo.Get(ctx, "case-other")
	if other != nil {
		t.Fatalf("other case plan = %+v", other)
	}
}
