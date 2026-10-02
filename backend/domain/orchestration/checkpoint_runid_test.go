package orchestration_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
)

func TestDispatch_CheckpointRunIDRemainsLogicalAcrossExecutionGenerations(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr, nil)
	cfg := &entity.MagiConfig{Code: "melchior"}
	case_ := &entity.DecisionCase{ID: "c1", ExecutionAttempt: 2, ExecutionGeneration: 7}
	d.Dispatch(context.Background(), case_, nil, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c1-melchior-r1-investigate"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}

	cr2 := &captureRuntime{}
	d2 := orchestration.NewDispatcher(cr2, nil)
	case_.ExecutionAttempt = 99
	case_.ExecutionGeneration = 8
	d2.Dispatch(context.Background(), case_, nil, []*entity.MagiConfig{cfg}, 1)
	if cr2.actx == nil || cr2.actx.RunID != want {
		t.Fatalf("next generation RunID=%q want logical RunID %q", cr2.actx.RunID, want)
	}
}
