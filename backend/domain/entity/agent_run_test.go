package entity

import "testing"

func TestUsageCost(t *testing.T) {
	u := &Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000}
	if got := u.Cost(2.5, 10); got != 7.5 {
		t.Fatalf("cost: %v", got)
	}
	if got := (&Usage{}).Cost(1, 1); got != 0 {
		t.Fatalf("empty cost: %v", got)
	}
	var nilU *Usage
	if got := nilU.Cost(1, 1); got != 0 {
		t.Fatalf("nil cost: %v", got)
	}
}
