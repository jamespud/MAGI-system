package execution

import "testing"

func TestStepIDStableAcrossAttempts(t *testing.T) {
	first := NewStepID("run-123", 7)
	second := NewStepID("run-123", 7)

	if first != second {
		t.Fatalf("step ID changed across attempts: first=%q second=%q", first, second)
	}
	if first != "8d30afea007391d3ad3a55473e4ef688d791a6b52482f918f240649bd55b0bec" {
		t.Fatalf("step ID = %q, want stable SHA-256 identity", first)
	}
}

func TestInvocationIDStableAcrossAttempts(t *testing.T) {
	first := NewInvocationID("step-abc", InvocationModel, 2)
	second := NewInvocationID("step-abc", InvocationModel, 2)

	if first != second {
		t.Fatalf("invocation ID changed across attempts: first=%q second=%q", first, second)
	}
	if first != "01a6bff23c6840698c56f16f5482346b340593c026c9d9d3d0a73aa754a2df68" {
		t.Fatalf("invocation ID = %q, want stable SHA-256 identity", first)
	}
}

func TestToolIdempotencyKeyStableAcrossAttempts(t *testing.T) {
	arguments := []byte(`{"q":"magi","limit":1}`)
	first := ToolIdempotencyKey("inv-abc", "search", arguments)
	second := ToolIdempotencyKey("inv-abc", "search", arguments)

	if first != second {
		t.Fatalf("tool idempotency key changed across attempts: first=%q second=%q", first, second)
	}
	if first != "1ef9b021e21fb4a3215d3885bb063cab0cf3b3ead0ba92947bef00734cb43358" {
		t.Fatalf("tool idempotency key = %q, want stable SHA-256 identity", first)
	}
}

func TestDifferentInvocationOrdinalsHaveDifferentIDs(t *testing.T) {
	first := NewInvocationID("step-abc", InvocationTool, 0)
	second := NewInvocationID("step-abc", InvocationTool, 1)

	if first == second {
		t.Fatalf("invocation IDs for different ordinals are equal: %q", first)
	}
}
