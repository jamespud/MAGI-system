package canonical

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// F33-06 ordering evidence: the struct path must reject over-budget values
// INSIDE the validation phase, i.e. before json.Marshal is ever called. These
// tests call validateInMemory directly (the function CanonicalizeStruct runs
// before marshalling) so the resource check order is observable without relying
// on unstable memory-peak assertions.
func TestValidateInMemoryStopsAtMemberBudget(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxMembers = 1
	budget := &inMemoryBudget{lim: lim}
	err := validateInMemory(reflect.ValueOf(map[string]any{"a": 1, "b": 2}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindTooManyMembers {
		t.Fatalf("err = %v, want %s", err, KindTooManyMembers)
	}
	if budget.members != 2 {
		t.Fatalf("validation visited %d members; expected it to stop at the second", budget.members)
	}
}

func TestValidateInMemoryStopsAtByteBudget(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxBytes = 32
	budget := &inMemoryBudget{lim: lim}
	err := validateInMemory(reflect.ValueOf(map[string]any{"s": strings.Repeat("x", 64)}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindPayloadTooLarge {
		t.Fatalf("err = %v, want %s", err, KindPayloadTooLarge)
	}
	if budget.bytes <= int64(lim.MaxBytes) {
		t.Fatalf("budget recorded %d bytes, expected it to exceed %d", budget.bytes, lim.MaxBytes)
	}
}

func TestValidateInMemoryCountsNestedMembers(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxMembers = 3
	budget := &inMemoryBudget{lim: lim}
	// 1 outer member + 3 inner members = 4 > 3.
	err := validateInMemory(reflect.ValueOf(map[string]any{"a": []any{1, 2, 3}}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindTooManyMembers {
		t.Fatalf("err = %v, want %s", err, KindTooManyMembers)
	}
}

type internalPtrChild struct{ N int }

func (*internalPtrChild) MarshalJSON() ([]byte, error) { return []byte(`{"n":999}`), nil }

type internalPlainChild struct{ N int }

// The pointer-receiver check must be visible to the validator, not only to
// json.Marshal.
func TestHasCustomMarshalerDetectsPointerReceiver(t *testing.T) {
	if !hasCustomMarshaler(reflect.TypeOf(internalPtrChild{})) {
		t.Fatal("pointer-receiver MarshalJSON was not detected")
	}
	if !hasCustomMarshaler(reflect.TypeOf(&internalPtrChild{})) {
		t.Fatal("*T pointer-receiver marshaler was not detected")
	}
	if hasCustomMarshaler(reflect.TypeOf(internalPlainChild{})) {
		t.Fatal("plain struct falsely reported as custom marshaler")
	}
}
