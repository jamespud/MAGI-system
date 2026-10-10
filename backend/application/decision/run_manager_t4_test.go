package decision

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestPositiveGenerationRetryResetRequiresOwnerCapability(t *testing.T) {
	m := &RunManager{}
	c := &entity.DecisionCase{ID: "case", Status: entity.CaseStatusInvestigating, ExecutionGeneration: 3}
	owner := &entity.ExecutionContext{CaseID: "case", JobID: "job", WorkerID: "worker", ExecutionGeneration: 3}
	for _, execution := range []*entity.ExecutionContext{nil, owner} {
		ok, err := m.resetCaseForRetry(context.Background(), c, execution)
		if ok || !errors.Is(err, port.ErrLeaseLost) || c.Status != entity.CaseStatusInvestigating {
			t.Fatalf("reset=%v err=%v Case=%+v", ok, err, c)
		}
	}
}
