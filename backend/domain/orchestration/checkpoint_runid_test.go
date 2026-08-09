package orchestration_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
)

func TestDispatch_CheckpointRunIDIgnoresExecutionAttempt(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr, nil)
	cfg := &entity.MagiConfig{Code: "melchior"}
	case_ := &entity.DecisionCase{ID: "c1", ExecutionAttempt: 2}
	d.Dispatch(context.Background(), case_, nil, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c1-melchior-r1-investigate"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}
}
