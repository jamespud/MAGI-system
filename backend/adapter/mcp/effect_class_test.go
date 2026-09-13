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
	// The triple mcp-go writes into every tool it creates. It equals the MCP
	// defaults for absent hints, so it carries no author intent.
	mcpGoDefault := mcpgo.ToolAnnotation{
		ReadOnlyHint: boolPtr(false), DestructiveHint: boolPtr(true), IdempotentHint: boolPtr(false),
	}
	cases := []struct {
		name string
		a    mcpgo.ToolAnnotation
		want port.ToolEffectClass
	}{
		{"read only", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(true)}, port.ToolEffectReadOnly},
		{"idempotent", mcpgo.ToolAnnotation{IdempotentHint: boolPtr(true)}, port.ToolEffectIdempotent},
		{"declared destructive", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(true)}, port.ToolEffectNonIdempotent},
		{"destructive and idempotent repeats safely", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(true), IdempotentHint: boolPtr(true)}, port.ToolEffectIdempotent},
		{"read only outranks destructive", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(true), DestructiveHint: boolPtr(true)}, port.ToolEffectReadOnly},
		{"unannotated", mcpgo.ToolAnnotation{}, port.ToolEffectUnknown},
		{"protocol defaults are not a declaration", mcpGoDefault, port.ToolEffectUnknown},
		{"read only false is not a declaration", mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(false)}, port.ToolEffectUnknown},
		{"explicit non-destructive is still unclassified", mcpgo.ToolAnnotation{DestructiveHint: boolPtr(false)}, port.ToolEffectUnknown},
		{"defaults plus an unrelated open world hint stay unclassified",
			mcpgo.ToolAnnotation{ReadOnlyHint: boolPtr(false), DestructiveHint: boolPtr(true), IdempotentHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
			port.ToolEffectUnknown},
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
	// A server that ships no annotations at all.
	srv.AddTool(mcpgo.Tool{Name: "bare", InputSchema: mcpgo.ToolInputSchema{Type: "object"}}, ok)
	// A server that declares destruction without also declaring the other
	// hints: this is a statement, not the protocol default, so config may not
	// downgrade it.
	srv.AddTool(mcpgo.Tool{
		Name:        "purge",
		InputSchema: mcpgo.ToolInputSchema{Type: "object"},
		Annotations: mcpgo.ToolAnnotation{DestructiveHint: boolPtr(true)},
	}, ok)

	a := newWithDial([]ServerConfig{{
		Name: "annotated", Transport: "stdio", Command: "x",
		EffectOverrides: map[string]string{
			"bare": "idempotent", "plain": "idempotent", "wipe": "read_only", "purge": "read_only",
		},
	}}, inProcessDial(srv))

	defs, err := a.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "search"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "wipe"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "plain"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "bare"},
		{Source: entity.ToolSourceMCP, Server: "annotated", ToolName: "purge"},
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
		"mcp_annotated_bare":   port.ToolEffectIdempotent,
		// mcp-go answered with the protocol's default hints, which carry no
		// intent, so the operator classification is honoured.
		"mcp_annotated_plain": port.ToolEffectIdempotent,
		"mcp_annotated_wipe":  port.ToolEffectReadOnly,
		// The server declared destruction explicitly, so the override is
		// refused rather than trusted.
		"mcp_annotated_purge": port.ToolEffectNonIdempotent,
	}
	for name, wantEffect := range want {
		if got[name] != wantEffect {
			t.Fatalf("tool %s effect class = %q, want %q (all: %v)", name, got[name], wantEffect, got)
		}
	}
}
