package orchestration_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type captureRuntime struct {
	actx *runtime.AgentContext
}

func (c *captureRuntime) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	c.actx = actx
	return &runtime.LoopResult{Status: runtime.LoopStatusCompleted}, nil
}

func TestDispatch_SetsInvestigateRunID(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr)
	cfg := &entity.MagiConfig{Code: "melchior"}
	d.Dispatch(context.Background(), &entity.DecisionCase{ID: "c1"}, nil, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c1-melchior-r1-investigate"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}
}

func TestDispatchReconsider_SetsReconsiderRunID(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr)
	cfg := &entity.MagiConfig{Code: "balthasar"}
	d.DispatchReconsider(context.Background(), &entity.DecisionCase{ID: "c2"}, nil, entity.DebatePacket{}, nil, []*entity.MagiConfig{cfg}, 2)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c2-balthasar-r2-reconsider"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}
}
