package canonical_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

type vectorFile struct {
	Profile  string `json:"profile"`
	Positive []struct {
		Name              string `json:"name"`
		ArtifactType      string `json:"artifact_type"`
		InputJSON         string `json:"input_json"`
		CanonicalBytesHex string `json:"canonical_bytes_hex"`
		Digest            string `json:"digest"`
		ArtifactID        string `json:"artifact_id"`
	} `json:"positive"`
	Negative []struct {
		Name              string `json:"name"`
		InputJSON         string `json:"input_json"`
		ExpectedErrorKind string `json:"expected_error_kind"`
	} `json:"negative"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden_vectors.json"))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if v.Profile != canonical.Profile {
		t.Fatalf("vector profile %q != canonical.Profile %q", v.Profile, canonical.Profile)
	}
	return v
}

func kindOf(t *testing.T, err error) string {
	t.Helper()
	var cerr *canonical.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("expected *canonical.Error, got %T: %v", err, err)
	}
	return cerr.Kind
}

func TestCanonicalGoldenVectors(t *testing.T) {
	for _, tc := range loadVectors(t).Positive {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := canonical.Canonicalize([]byte(tc.InputJSON))
			if err != nil {
				t.Fatalf("Canonicalize(%s): %v", tc.InputJSON, err)
			}
			if hex.EncodeToString(got.Bytes()) != tc.CanonicalBytesHex {
				t.Fatalf("canonical bytes\n got %s\nwant %s", hex.EncodeToString(got.Bytes()), tc.CanonicalBytesHex)
			}
			digest, err := got.Digest(canonical.ArtifactType(tc.ArtifactType))
			if err != nil {
				t.Fatalf("Digest(%s): %v", tc.ArtifactType, err)
			}
			if digest != tc.Digest {
				t.Fatalf("digest\n got %s\nwant %s", digest, tc.Digest)
			}
			if tc.ArtifactID != "" {
				id, err := got.ArtifactID(canonical.ArtifactType(tc.ArtifactType))
				if err != nil || id != tc.ArtifactID {
					t.Fatalf("artifact ID got %q (%v), want %q", id, err, tc.ArtifactID)
				}
			}
		})
	}
}

func TestCanonicalRejectsInvalidInput(t *testing.T) {
	for _, tc := range loadVectors(t).Negative {
		t.Run(tc.Name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Canonicalize panicked on %s: %v", tc.InputJSON, r)
				}
			}()
			got, err := canonical.Canonicalize([]byte(tc.InputJSON))
			if err == nil {
				t.Fatalf("expected %s, canonicalized to %s", tc.ExpectedErrorKind, hex.EncodeToString(got.Bytes()))
			}
			if kind := kindOf(t, err); kind != tc.ExpectedErrorKind {
				t.Fatalf("error kind %q, want %q (%v)", kind, tc.ExpectedErrorKind, err)
			}
		})
	}
}

// Raw byte-level negatives that cannot be represented in the UTF-8 JSON vector
// file.
func TestCanonicalRejectsRawByteViolations(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"invalid utf8", []byte("{\"s\":\"\xff\"}"), canonical.KindInvalidUTF8},
		{"bare newline in string", []byte("{\"s\":\"a\nb\"}"), canonical.KindInvalidJSON},
		{"bare tab in string", []byte("{\"s\":\"a\tb\"}"), canonical.KindInvalidJSON},
		{"bare 0x01 in string", []byte("{\"s\":\"a\x01b\"}"), canonical.KindInvalidJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := canonical.Canonicalize(tc.in); err == nil {
				t.Fatal("expected rejection")
			} else if kind := kindOf(t, err); kind != tc.want {
				t.Fatalf("kind %q, want %q (%v)", kind, tc.want, err)
			}
		})
	}
}

// F33-01: the in-memory path must not launder values that encoding/json would
// silently render as something else.
func TestCanonicalizeStructRejectsLossyInput(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"float64 whole number", map[string]any{"n": float64(1.0)}, canonical.KindFloatNumber},
		{"float64 fraction", map[string]any{"n": 1.5}, canonical.KindFloatNumber},
		{"float32 nested", map[string]any{"a": map[string]any{"b": float32(0.5)}}, canonical.KindFloatNumber},
		{"float in slice", map[string]any{"a": []any{1, 2.5}}, canonical.KindFloatNumber},
		{"invalid utf8 string", map[string]any{"s": string([]byte{0xff})}, canonical.KindInvalidUTF8},
		{"invalid utf8 nested", map[string]any{"a": []any{map[string]any{"s": string([]byte{0xfe})}}}, canonical.KindInvalidUTF8},
		{"out of range int", map[string]any{"n": int64(1) << 53}, canonical.KindIntegerOutOfRange},
		{"non nfc string", map[string]any{"s": "e\u0301"}, canonical.KindNotNFC},
		{"[]byte implicit base64", map[string]any{"b": []byte("x")}, canonical.KindUnsupportedValue},
		{"complex", map[string]any{"c": complex(1, 2)}, canonical.KindUnsupportedValue},
		{"non string map key", map[string]any{"a": map[int]any{1: "x"}}, canonical.KindUnsupportedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := canonical.CanonicalizeStruct(tc.in); err == nil {
				t.Fatal("expected rejection")
			} else if kind := kindOf(t, err); kind != tc.want {
				t.Fatalf("kind %q, want %q (%v)", kind, tc.want, err)
			}
		})
	}
	// The safe counterpart must still work.
	got, err := canonical.CanonicalizeStruct(map[string]any{"n": 1, "s": "ok"})
	if err != nil {
		t.Fatalf("valid struct rejected: %v", err)
	}
	if got.String() != `{"n":1,"s":"ok"}` {
		t.Fatalf("unexpected canonical form: %s", got.String())
	}
}

// F33-02: identity can only be derived from a value this package canonicalized.
func TestAdoptRejectsNonCanonicalBytes(t *testing.T) {
	nonCanonical := []struct {
		name string
		in   string
	}{
		{"reordered keys", `{"b":2,"a":1}`},
		{"whitespace", `{ "a":1}`},
		{"duplicate keys", `{"a":1,"a":2}`},
		{"float", `{"n":1.0}`},
		{"empty payload", ``},
		{"escaped html", `{"s":"\u003c"}`},
	}
	for _, tc := range nonCanonical {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := canonical.Adopt([]byte(tc.in)); err == nil {
				t.Fatalf("Adopt accepted non-canonical bytes: %s", tc.in)
			}
		})
	}
	canon, err := canonical.Canonicalize([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := canonical.Adopt(canon.Bytes())
	if err != nil {
		t.Fatalf("Adopt rejected canonical bytes: %v", err)
	}
	want, _ := canon.Digest(canonical.TypeDecisionCharter)
	got, _ := adopted.Digest(canonical.TypeDecisionCharter)
	if want != got {
		t.Fatalf("round-tripped identity changed: %s != %s", got, want)
	}
}

func TestZeroCanonicalHasNoIdentity(t *testing.T) {
	var zero canonical.Canonical
	if !zero.IsZero() {
		t.Fatal("zero value must be detectable")
	}
	if _, err := zero.Digest(canonical.TypeDecisionCharter); err == nil {
		t.Fatal("zero Canonical must not produce a digest")
	}
	if _, err := zero.ArtifactID(canonical.TypeDecisionCharter); err == nil {
		t.Fatal("zero Canonical must not produce an artifact id")
	}
}

// Semantic equality must give identity equality even when the raw JSON differs.
func TestEquivalentJSONHasSameIdentity(t *testing.T) {
	a, err := canonical.Canonicalize([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonical.Canonicalize([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	da, _ := a.Digest(canonical.TypeDecisionCharter)
	db, _ := b.Digest(canonical.TypeDecisionCharter)
	if da != db {
		t.Fatalf("equivalent documents produced different identities: %s != %s", da, db)
	}
}

// F33-03: resource ceilings are enforced before/while parsing, and exact
// boundary values are still accepted.
func TestLimitsAreEnforced(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		lim := canonical.DefaultLimits()
		lim.MaxBytes = 16
		if _, err := canonical.CanonicalizeWithLimits([]byte(`{"a":1}`), lim); err != nil {
			t.Fatalf("within limit rejected: %v", err)
		}
		_, err := canonical.CanonicalizeWithLimits([]byte(`{"aaaaaaaaaaaa":1}`), lim)
		if kind := kindOf(t, err); kind != canonical.KindPayloadTooLarge {
			t.Fatalf("kind %q, want %q", kind, canonical.KindPayloadTooLarge)
		}
	})
	t.Run("depth", func(t *testing.T) {
		lim := canonical.DefaultLimits()
		lim.MaxDepth = 3
		if _, err := canonical.CanonicalizeWithLimits([]byte(`{"a":{"b":1}}`), lim); err != nil {
			t.Fatalf("within depth rejected: %v", err)
		}
		_, err := canonical.CanonicalizeWithLimits([]byte(`{"a":{"b":{"c":1}}}`), lim)
		if kind := kindOf(t, err); kind != canonical.KindTooDeep {
			t.Fatalf("kind %q, want %q", kind, canonical.KindTooDeep)
		}
	})
	t.Run("members", func(t *testing.T) {
		lim := canonical.DefaultLimits()
		lim.MaxMembers = 2
		if _, err := canonical.CanonicalizeWithLimits([]byte(`{"a":1,"b":2}`), lim); err != nil {
			t.Fatalf("within member limit rejected: %v", err)
		}
		_, err := canonical.CanonicalizeWithLimits([]byte(`{"a":1,"b":2,"c":3}`), lim)
		if kind := kindOf(t, err); kind != canonical.KindTooManyMembers {
			t.Fatalf("kind %q, want %q", kind, canonical.KindTooManyMembers)
		}
	})
	t.Run("string bytes", func(t *testing.T) {
		lim := canonical.DefaultLimits()
		lim.MaxStringBytes = 4
		if _, err := canonical.CanonicalizeWithLimits([]byte(`{"s":"abcd"}`), lim); err != nil {
			t.Fatalf("within string limit rejected: %v", err)
		}
		_, err := canonical.CanonicalizeWithLimits([]byte(`{"s":"abcde"}`), lim)
		if kind := kindOf(t, err); kind != canonical.KindStringTooLong {
			t.Fatalf("kind %q, want %q", kind, canonical.KindStringTooLong)
		}
	})
}

func TestDigestDomainSeparation(t *testing.T) {
	got, err := canonical.Canonicalize([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	charter, err := got.Digest(canonical.TypeDecisionCharter)
	if err != nil {
		t.Fatal(err)
	}
	record, err := got.Digest(canonical.TypeDecisionRecord)
	if err != nil {
		t.Fatal(err)
	}
	if charter == record {
		t.Fatal("identical bytes produced the same digest for two artifact types")
	}
	if _, err := got.Digest(canonical.ArtifactType("not_a_type")); err == nil {
		t.Fatal("unknown artifact type must be rejected")
	}
	if _, err := got.Digest(canonical.ArtifactType("decision_charter\nx")); err == nil {
		t.Fatal("artifact type containing a newline must be rejected")
	}
}

func TestArtifactIDDerivedFromDigest(t *testing.T) {
	got, err := canonical.Canonicalize([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := got.Digest(canonical.TypeDecisionCharter)
	if err != nil {
		t.Fatal(err)
	}
	id, err := got.ArtifactID(canonical.TypeDecisionCharter)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "chtr_") || !strings.HasSuffix(id, digest) {
		t.Fatalf("artifact id %q is not derived from digest %q", id, digest)
	}
}

func TestCanonicalizeIsIdempotent(t *testing.T) {
	for _, in := range []string{`{"b":1,"a":[1,2,{"d":null}]}`, `[]`, `{}`, `{"s":"<x>&\n"}`} {
		once, err := canonical.Canonicalize([]byte(in))
		if err != nil {
			t.Fatalf("Canonicalize(%s): %v", in, err)
		}
		twice, err := canonical.Canonicalize(once.Bytes())
		if err != nil {
			t.Fatalf("Canonicalize(canonical(%s)): %v", in, err)
		}
		if once.String() != twice.String() {
			t.Fatalf("not idempotent: %s -> %s", once.String(), twice.String())
		}
	}
}

func TestCanonicalizeStructMatchesJSONPath(t *testing.T) {
	type sample struct {
		B int    `json:"b"`
		A string `json:"a"`
	}
	v := sample{B: 2, A: "x"}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := canonical.Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	fromStruct, err := canonical.CanonicalizeStruct(v)
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON.String() != fromStruct.String() {
		t.Fatalf("struct path %s != json path %s", fromStruct.String(), fromJSON.String())
	}
	if fromStruct.String() != `{"a":"x","b":2}` {
		t.Fatalf("unexpected canonical form: %s", fromStruct.String())
	}
}

func TestExplicitNullDiffersFromAbsent(t *testing.T) {
	nullValue, err := canonical.Canonicalize([]byte(`{"a":null}`))
	if err != nil {
		t.Fatal(err)
	}
	absent, err := canonical.Canonicalize([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if nullValue.String() == absent.String() {
		t.Fatal("explicit null collapsed into absent field")
	}
}

// Cross-language check. The hard gate runs in CI (see .github/workflows/ci.yml);
// locally this degrades to a skip when python3 is absent.
func TestGoldenVectorsReproduceInPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; cross-language verification is enforced by the CI gate")
	}
	cmd := exec.Command("python3", "testdata/verify_golden.py", "testdata/golden_vectors.json")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python reference verifier failed: %v\n%s", err, out)
	}
}

// --- F33-05: pointer-receiver marshalers ------------------------------------

type ptrMarshalChild struct {
	N int `json:"n"`
}

// Pointer receiver only: encoding/json calls this on addressable struct fields
// even though the value type does not implement json.Marshaler.
func (*ptrMarshalChild) MarshalJSON() ([]byte, error) { return []byte(`{"n":999}`), nil }

type ptrMarshalParent struct {
	Child ptrMarshalChild `json:"child"`
}

type textMarshalChild struct{ S string }

func (*textMarshalChild) MarshalText() ([]byte, error) { return []byte("rewritten"), nil }

type plainChild struct {
	N int `json:"n"`
}

type plainParent struct {
	Child plainChild `json:"child"`
}

func TestCanonicalizeStructRejectsPointerReceiverMarshaler(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"addressable struct field", &ptrMarshalParent{Child: ptrMarshalChild{N: 1}}},
		{"value struct field", ptrMarshalParent{Child: ptrMarshalChild{N: 1}}},
		{"nested pointer field", &struct {
			Child *ptrMarshalChild `json:"child"`
		}{Child: &ptrMarshalChild{N: 1}}},
		{"interface holding value", map[string]any{"c": ptrMarshalChild{N: 1}}},
		{"interface holding pointer", map[string]any{"c": &ptrMarshalChild{N: 1}}},
		{"slice element", []any{ptrMarshalChild{N: 1}}},
		{"pointer receiver text marshaler", &struct {
			C textMarshalChild `json:"c"`
		}{C: textMarshalChild{S: "x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonical.CanonicalizeStruct(tc.in)
			if err == nil {
				t.Fatalf("accepted a custom marshaler; canonicalized to %s", got.String())
			}
			if kind := kindOf(t, err); kind != canonical.KindCustomMarshaler {
				t.Fatalf("kind %q, want %q (%v)", kind, canonical.KindCustomMarshaler, err)
			}
		})
	}
	// Legal counterpart: the same shapes without a custom marshaler still work.
	got, err := canonical.CanonicalizeStruct(&plainParent{Child: plainChild{N: 1}})
	if err != nil {
		t.Fatalf("plain struct rejected: %v", err)
	}
	if got.String() != `{"child":{"n":1}}` {
		t.Fatalf("unexpected canonical form: %s", got.String())
	}
}

// --- F33-06: struct path resource ceilings ----------------------------------

func TestCanonicalizeStructEnforcesLimits(t *testing.T) {
	lim := canonical.DefaultLimits()
	lim.MaxMembers = 2
	if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"a": 1, "b": 2}, lim); err != nil {
		t.Fatalf("at member limit rejected: %v", err)
	}
	if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"a": 1, "b": 2, "c": 3}, lim); kindOf(t, err) != canonical.KindTooManyMembers {
		t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindTooManyMembers)
	}
	if _, err := canonical.CanonicalizeStructWithLimits([]any{1, 2, 3}, lim); kindOf(t, err) != canonical.KindTooManyMembers {
		t.Fatalf("slice members: kind %q, want %q", kindOf(t, err), canonical.KindTooManyMembers)
	}

	t.Run("map key length", func(t *testing.T) {
		l := canonical.DefaultLimits()
		l.MaxStringBytes = 3
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"abc": 1}, l); err != nil {
			t.Fatalf("at key limit rejected: %v", err)
		}
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"abcd": 1}, l); kindOf(t, err) != canonical.KindStringTooLong {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindStringTooLong)
		}
	})

	t.Run("byte budget before marshalling", func(t *testing.T) {
		l := canonical.DefaultLimits()
		l.MaxBytes = 32
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"a": 1}, l); err != nil {
			t.Fatalf("small object rejected: %v", err)
		}
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"s": strings.Repeat("x", 64)}, l); kindOf(t, err) != canonical.KindPayloadTooLarge {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindPayloadTooLarge)
		}
	})

	t.Run("depth", func(t *testing.T) {
		l := canonical.DefaultLimits()
		l.MaxDepth = 3
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"a": map[string]any{"b": 1}}, l); err != nil {
			t.Fatalf("within depth rejected: %v", err)
		}
		if _, err := canonical.CanonicalizeStructWithLimits(map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}, l); kindOf(t, err) != canonical.KindTooDeep {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindTooDeep)
		}
	})

	t.Run("array separators count toward the byte budget", func(t *testing.T) {
		l := canonical.DefaultLimits()
		l.MaxBytes = 10
		if _, err := canonical.CanonicalizeStructWithLimits([]any{nil, nil}, l); kindOf(t, err) != canonical.KindPayloadTooLarge {
			t.Fatalf("kind %q, want %q ([null,null] is 11 bytes)", kindOf(t, err), canonical.KindPayloadTooLarge)
		}
	})

	t.Run("json tag names count toward the string budget", func(t *testing.T) {
		type tagged struct {
			A int `json:"averylongfieldname"`
		}
		l := canonical.DefaultLimits()
		l.MaxStringBytes = 5
		if _, err := canonical.CanonicalizeStructWithLimits(tagged{A: 1}, l); kindOf(t, err) != canonical.KindStringTooLong {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindStringTooLong)
		}
	})

	t.Run("json dash fields are not counted", func(t *testing.T) {
		type withIgnored struct {
			A int `json:"a"`
			B int `json:"-"`
		}
		l := canonical.DefaultLimits()
		l.MaxMembers = 1
		got, err := canonical.CanonicalizeStructWithLimits(withIgnored{A: 1, B: 2}, l)
		if err != nil {
			t.Fatalf("json:\"-\" field consumed budget: %v", err)
		}
		if got.String() != `{"a":1}` {
			t.Fatalf("unexpected canonical form: %s", got.String())
		}
	})

	t.Run("json string option is rejected", func(t *testing.T) {
		type stringOpt struct {
			N int `json:"n,string"`
		}
		if _, err := canonical.CanonicalizeStruct(stringOpt{N: 1}); kindOf(t, err) != canonical.KindUnsupportedValue {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindUnsupportedValue)
		}
	})

	t.Run("invalid json tag names are rejected", func(t *testing.T) {
		// encoding/json ignores tag names that fail its character rules and
		// falls back to the Go field name, so budgeting the tag would be wrong.
		type badTag struct {
			ThisIsAnExtremelyLongFieldName bool `json:"🧿"`
		}
		if _, err := canonical.CanonicalizeStruct(badTag{}); kindOf(t, err) != canonical.KindUnsupportedValue {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindUnsupportedValue)
		}
		// Punctuation accepted by encoding/json stays valid.
		type okTag struct {
			N int `json:"n-ok_1"`
		}
		got, err := canonical.CanonicalizeStruct(okTag{N: 1})
		if err != nil {
			t.Fatalf("valid punctuation tag rejected: %v", err)
		}
		if got.String() != `{"n-ok_1":1}` {
			t.Fatalf("unexpected canonical form: %s", got.String())
		}
	})

	t.Run("embedded structs are rejected", func(t *testing.T) {
		type Embedded struct {
			N int `json:"n"`
		}
		type Outer struct {
			Embedded
			M int `json:"m"`
		}
		if _, err := canonical.CanonicalizeStruct(Outer{Embedded: Embedded{N: 1}, M: 2}); kindOf(t, err) != canonical.KindUnsupportedValue {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindUnsupportedValue)
		}
	})

	t.Run("struct fields consume member budget", func(t *testing.T) {
		l := canonical.DefaultLimits()
		l.MaxMembers = 1
		type oneField struct {
			A int `json:"a"`
		}
		if _, err := canonical.CanonicalizeStructWithLimits(oneField{A: 1}, l); err != nil {
			t.Fatalf("single field rejected: %v", err)
		}
		type twoFields struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if _, err := canonical.CanonicalizeStructWithLimits(twoFields{A: 1, B: 2}, l); kindOf(t, err) != canonical.KindTooManyMembers {
			t.Fatalf("kind %q, want %q", kindOf(t, err), canonical.KindTooManyMembers)
		}
	})
}
