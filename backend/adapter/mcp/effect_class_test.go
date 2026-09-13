package mcp

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpgo_server "github.com/mark3labs/mcp-go/server"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func boolPtr(v bool) *bool { return &v }

func TestEffectClassFromAnnotations(t *testing.T) {
	cases := []struct {
		name string
		a    mcpgo.ToolAnnotation
		want port.ToolEffectClass
	}{
		{"read only", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(true)}, port.ToolEffectReadOnly},
		{"destructive", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(true)}, port.ToolEffectNonIdempotent},
		{"idempotent", mcpgo.ToolAnnotation{IdempotentHint: boolPtr(true)}, port.ToolEffectIdempotent},
		{"read only outranks destructive", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(true), DestructiveHint: boolPtr(true)}, port.ToolEffectReadOnly},
		{"destructive outranks idempotent", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(true), IdempotentHint: boolPtr(true)}, port.ToolEffectNonIdempotent},
		{"unannotated", mcpgo.ToolAnnotation{}, port.ToolEffectUnknown},
		{"explicit non-destructive is still unclassified", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(false)}, port.ToolEffectUnknown},
		{"read only false is still unclassified", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(false)}, port.ToolEffectUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectClassFromAnnotations(tc.a); got != tc.want {
				t.Fatalf("effect class = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeEffectClass_OverrideCannotLowerSafety(t *testing.T) {
	cases := []struct {
		name       string
		annotation port.ToolEffectClass
		override   string
		want       port.ToolEffectClass
	}{
		{"override classifies an unannotated tool", port.ToolEffectUnknown, "read_only", port.ToolEffectReadOnly},
		{"override classifies a missing annotation", "", "idempotent", port.ToolEffectIdempotent},
		{"server annotation outranks a looser override", port.ToolEffectNonIdempotent, "read_only", port.ToolEffectNonIdempotent},
		{"server annotation outranks an idempotent override", port.ToolEffectNonIdempotent, "idempotent", port.ToolEffectNonIdempotent},
		{"a more conservative override is allowed", port.ToolEffectReadOnly, "non_idempotent", port.ToolEffectNonIdempotent},
		{"a conservative override applies to an annotated tool", port.ToolEffectIdempotent, "unknown", port.ToolEffectUnknown},
		{"an unparseable override is ignored", port.ToolEffectReadOnly, "yolo", port.ToolEffectReadOnly},
		{"no override keeps the annotation", port.ToolEffectIdempotent, "", port.ToolEffectIdempotent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeEffectClass("srv", "tool", tc.annotation, tc.override); got != tc.want {
				t.Fatalf("merged effect class = %q, want %q", got, tc.want)
			}
		})
	}
}

// The definition the kernel derives retry safety from has to carry the effect
// class, including the operator override, or the adapter and the kernel would
// disagree about whether a call may be repeated.
func TestAdapter_ListCarriesEffectClassAndOverride(t *testing.T) {
	srv := mcpgo_server.NewMCPServer("annotated", "1.0.0")
	ok := func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return &mcpgo.CallToolResult{}, nil
	}
	srv.AddTool(mcpgo.NewTool("search", mcpgo.WithReadOnlyHintAnnotation(true)), ok)
	srv.AddTool(mcpgo.NewTool("wipe", mcpgo.WithDestructiveHintAnnotation(true)), ok)
	srv.AddTool(mcpgo.NewTool("plain"), ok)
	// A server that ships no annotations at all. mcp-go's own NewTool always
	// fills in the protocol defaults (readOnly=false, destructive=true), so
	// "plain" below is a declared-destructive tool even though its author never
	// said anything: only a tool built without those defaults can be classified
	// by config.
	srv.AddTool(mcpgo.Tool{Name: "bare", InputSchema: mcpgo.ToolInputSchema{Type: "object"}}, ok)

	a := newWithDial([]ServerConfig{{
		Name: "annotated", Transport: "stdio", Command: "x",
		EffectOverrides: map[string]string{"bare": "idempotent", "plain": "idempotent", "wipe": "read_only"},
	}}, inProcessDial(srv))

	defs, err := a.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "search"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "wipe"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "plain"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "bare"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := make(map[string]port.ToolEffectClass, len(defs))
	for _, def := range defs {
		got[def.Name] = def.EffectClass
	}
	want := map[string]port.ToolEffectClass{
		"mcp_annotated_search": port.ToolEffectReadOnly,
		"mcp_annotated_wipe":   port.ToolEffectNonIdempotent,
		"mcp_annotated_bare":   port.ToolEffectIdempotent,
		// mcp-go answered with the protocol's default destructive hint, so the
		// override is refused rather than trusted.
		"mcp_annotated_plain": port.ToolEffectNonIdempotent,
	}
	for name, wantEffect := range want {
		if got[name] != wantEffect {
			t.Fatalf("tool %s effect class = %q, want %q (all: %v)", name, got[name], wantEffect, got)
		}
	}
}
