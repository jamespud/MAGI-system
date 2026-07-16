package tool_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubToolReg struct {
	defs []port.ToolDefinition
}

func (s *stubToolReg) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	return s.defs, nil
}

func TestToolService_List(t *testing.T) {
	reg := &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add"}}}
	svc := tool.NewService(reg)
	defs, err := svc.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "calc" {
		t.Fatalf("expected calc, got: %+v", defs)
	}
}

func TestToolService_Get(t *testing.T) {
	reg := &stubToolReg{defs: []port.ToolDefinition{{Name: "calc"}, {Name: "search"}}}
	svc := tool.NewService(reg)
	def, err := svc.Get(context.Background(), "search")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if def == nil || def.Name != "search" {
		t.Fatalf("expected search, got: %+v", def)
	}
}
