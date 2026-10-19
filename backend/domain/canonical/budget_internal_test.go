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

// F33-07: the budget must include array/object separators. With MaxBytes=10 the
// old accounting allowed `[null,null]` (budget 10, actual 11) into json.Marshal.
func TestValidateInMemoryCountsArraySeparators(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxBytes = 10
	budget := &inMemoryBudget{lim: lim}
	err := validateInMemory(reflect.ValueOf([]any{nil, nil}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindPayloadTooLarge {
		t.Fatalf("err = %v, want %s (budget=%d)", err, KindPayloadTooLarge, budget.bytes)
	}
	if budget.bytes < 11 {
		t.Fatalf("budget=%d is not a strict upper bound for [null,null] (11 bytes)", budget.bytes)
	}
}

type taggedPayload struct {
	A int `json:"averylongfieldname"`
}

// F33-07: the key budget must use the json tag name, not field.Name.
func TestValidateInMemoryUsesJSONTagName(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxStringBytes = 5
	budget := &inMemoryBudget{lim: lim}
	err := validateInMemory(reflect.ValueOf(taggedPayload{A: 1}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindStringTooLong {
		t.Fatalf("err = %v, want %s", err, KindStringTooLong)
	}

	lim.MaxStringBytes = len("averylongfieldname")
	budget = &inMemoryBudget{lim: lim}
	if err := validateInMemory(reflect.ValueOf(taggedPayload{A: 1}), "$", 1, budget); err != nil {
		t.Fatalf("tag name at the exact limit rejected: %v", err)
	}
}

type embeddedChild struct {
	Hidden float64 `json:"hidden"`
}

type embeddedParent struct {
	embeddedChild
	N int `json:"n"`
}

// An unexported embedded struct is still marshalled field-by-field by
// encoding/json, so it must not be skipped by validation.
func TestValidateInMemoryRejectsEmbeddedStructs(t *testing.T) {
	budget := &inMemoryBudget{lim: DefaultLimits()}
	err := validateInMemory(reflect.ValueOf(embeddedParent{N: 1}), "$", 1, budget)
	var cerr *Error
	if !errors.As(err, &cerr) || cerr.Kind != KindUnsupportedValue {
		t.Fatalf("err = %v, want %s", err, KindUnsupportedValue)
	}
}
