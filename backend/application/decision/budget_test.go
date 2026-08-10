package decision_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

type fakeBudgetChecker struct {
	info *decision.BudgetExceededInfo
	err  error
}

func (f *fakeBudgetChecker) CheckBudget(ctx context.Context, userID int64) (*decision.BudgetExceededInfo, error) {
	return f.info, f.err
}

func TestRunManager_BudgetBlocksStart(t *testing.T) {
	rm := decision.NewRunManager(&countingOrchestrator{}, decision.RunManagerDeps{
		BudgetChecker: &fakeBudgetChecker{info: &decision.BudgetExceededInfo{TokensExceeded: true}},
	})
	err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c-budget", UserID: 7})
	if !errors.Is(err, decision.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestRunManager_BudgetAllowsStart(t *testing.T) {
	rm := decision.NewRunManager(&countingOrchestrator{}, decision.RunManagerDeps{
		BudgetChecker: &fakeBudgetChecker{info: &decision.BudgetExceededInfo{}},
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c-ok", UserID: 7}); err != nil {
		t.Fatalf("start within budget: %v", err)
	}
	rm.Cancel("c-ok")
}

func TestRunManager_BudgetCheckErrorPropagates(t *testing.T) {
	rm := decision.NewRunManager(&countingOrchestrator{}, decision.RunManagerDeps{
		BudgetChecker: &fakeBudgetChecker{err: errors.New("usage store unavailable")},
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c-err", UserID: 7}); err == nil {
		t.Fatal("expected budget-check error")
	}
}
