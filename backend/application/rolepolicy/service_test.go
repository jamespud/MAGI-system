package rolepolicy_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/rolepolicy"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memRolePolicyRepo struct {
	byCode map[string]entity.RolePolicy
}

func (m *memRolePolicyRepo) Get(ctx context.Context, code string) (*entity.RolePolicy, error) {
	if p, ok := m.byCode[code]; ok {
		return &p, nil
	}
	return nil, nil
}
func (m *memRolePolicyRepo) Save(ctx context.Context, code string, p entity.RolePolicy) error {
	m.byCode[code] = p
	return nil
}

func TestService_GetFallsBackToDefault(t *testing.T) {
	svc := rolepolicy.NewService(&memRolePolicyRepo{byCode: map[string]entity.RolePolicy{}})
	p, err := svc.Get(context.Background(), "melchior")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.RequiredAssessment != entity.RoleAssessmentTechnical || !p.EnforceAssessment {
		t.Fatalf("default = %+v", p)
	}
	if _, err := svc.Get(context.Background(), "unknown"); err == nil {
		t.Fatal("unknown role must fail")
	}
}

func TestService_SaveAndListAndReset(t *testing.T) {
	repo := &memRolePolicyRepo{byCode: map[string]entity.RolePolicy{}}
	svc := rolepolicy.NewService(repo)
	ctx := context.Background()
	updated := entity.RolePolicy{
		Role: "melchior", EnforceAssessment: true,
		RequiredAssessment: entity.RoleAssessmentTechnical, MinTechnicalScore: 80,
	}
	got, err := svc.Save(ctx, "melchior", updated)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.MinTechnicalScore != 80 {
		t.Fatalf("saved = %+v", got)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list = %d", len(list))
	}

	if _, err := svc.Save(ctx, "balthasar", entity.RolePolicy{Role: "balthasar", EnforceAssessment: true, RequiredAssessment: "bogus"}); err == nil {
		t.Fatal("invalid assessment must be rejected")
	}
	if _, err := svc.Save(ctx, "melchior", entity.RolePolicy{Role: "casper", EnforceAssessment: true, RequiredAssessment: entity.RoleAssessmentTechnical}); err == nil {
		t.Fatal("role key mismatch must be rejected")
	}

	reset, err := svc.Reset(ctx, "melchior")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.MinTechnicalScore != 60 {
		t.Fatalf("reset = %+v", reset)
	}
}

var _ port.RolePolicyRepository = (*memRolePolicyRepo)(nil)
