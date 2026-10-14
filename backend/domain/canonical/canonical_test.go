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

// Every positive vector must canonicalize to the exact bytes and digest produced
// by the independent reference implementation.
func TestCanonicalGoldenVectors(t *testing.T) {
	vectors := loadVectors(t)
	for _, tc := range vectors.Positive {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := canonical.Canonicalize([]byte(tc.InputJSON))
			if err != nil {
				t.Fatalf("Canonicalize(%s): %v", tc.InputJSON, err)
			}
			if hex.EncodeToString(got) != tc.CanonicalBytesHex {
				t.Fatalf("canonical bytes\n got %s\nwant %s", hex.EncodeToString(got), tc.CanonicalBytesHex)
			}
			digest, err := canonical.Digest(canonical.ArtifactType(tc.ArtifactType), got)
			if err != nil {
				t.Fatalf("Digest(%s): %v", tc.ArtifactType, err)
			}
			if digest != tc.Digest {
				t.Fatalf("digest\n got %s\nwant %s", digest, tc.Digest)
			}
		})
	}
}

// Negative vectors must fail with the exact error class, never panic and never
// silently accept the value.
func TestCanonicalRejectsInvalidInput(t *testing.T) {
	vectors := loadVectors(t)
	for _, tc := range vectors.Negative {
		t.Run(tc.Name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Canonicalize panicked on %s: %v", tc.InputJSON, r)
				}
			}()
			got, err := canonical.Canonicalize([]byte(tc.InputJSON))
			if err == nil {
				t.Fatalf("expected %s, canonicalized to %s", tc.ExpectedErrorKind, hex.EncodeToString(got))
			}
			var cerr *canonical.Error
			if !errors.As(err, &cerr) {
				t.Fatalf("expected *canonical.Error, got %T: %v", err, err)
			}
			if cerr.Kind != tc.ExpectedErrorKind {
				t.Fatalf("error kind %q, want %q (%v)", cerr.Kind, tc.ExpectedErrorKind, err)
			}
		})
	}
}

// The same canonical bytes under two artifact types must not share an identity:
// domain separation is structural.
func TestDigestDomainSeparation(t *testing.T) {
	canon := []byte(`{"a":1}`)
	charter, err := canonical.Digest(canonical.TypeDecisionCharter, canon)
	if err != nil {
		t.Fatal(err)
	}
	record, err := canonical.Digest(canonical.TypeDecisionRecord, canon)
	if err != nil {
		t.Fatal(err)
	}
	if charter == record {
		t.Fatal("identical bytes produced the same digest for two artifact types")
	}
	if _, err := canonical.Digest(canonical.ArtifactType("not_a_type"), canon); err == nil {
		t.Fatal("unknown artifact type must be rejected")
	}
	if _, err := canonical.Digest(canonical.ArtifactType("decision_charter\nx"), canon); err == nil {
		t.Fatal("artifact type containing a newline must be rejected")
	}
}

func TestArtifactIDDerivedFromDigest(t *testing.T) {
	canon := []byte(`{"a":1}`)
	digest, err := canonical.Digest(canonical.TypeDecisionCharter, canon)
	if err != nil {
		t.Fatal(err)
	}
	id, err := canonical.ArtifactID(canonical.TypeDecisionCharter, canon)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "chtr_") || !strings.HasSuffix(id, digest) {
		t.Fatalf("artifact id %q is not derived from digest %q", id, digest)
	}
	if _, err := canonical.ArtifactID(canonical.ArtifactType("nope"), canon); err == nil {
		t.Fatal("unknown type must not yield an artifact id")
	}
}

func TestCanonicalizeIsIdempotent(t *testing.T) {
	inputs := []string{`{"b":1,"a":[1,2,{"d":null}]}`, `[]`, `{}`, `{"s":"<x>&\n"}`}
	for _, in := range inputs {
		once, err := canonical.Canonicalize([]byte(in))
		if err != nil {
			t.Fatalf("Canonicalize(%s): %v", in, err)
		}
		twice, err := canonical.Canonicalize(once)
		if err != nil {
			t.Fatalf("Canonicalize(canonical(%s)): %v", in, err)
		}
		if string(once) != string(twice) {
			t.Fatalf("not idempotent: %s -> %s -> %s", in, once, twice)
		}
	}
}

// CanonicalizeStruct must agree with Canonicalize over the struct's JSON form so
// there is exactly one canonicalization path for artifacts.
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
	if string(fromJSON) != string(fromStruct) {
		t.Fatalf("struct path %s != json path %s", fromStruct, fromJSON)
	}
	if string(fromStruct) != `{"a":"x","b":2}` {
		t.Fatalf("unexpected canonical form: %s", fromStruct)
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
	if string(nullValue) == string(absent) {
		t.Fatal("explicit null collapsed into absent field")
	}
}

// Cross-language check: the independent Python reference must reproduce the same
// vectors. Skipped (and reported) when python3 is unavailable.
func TestGoldenVectorsReproduceInPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; cross-language verification was NOT executed")
	}
	cmd := exec.Command("python3", "testdata/verify_golden.py", "testdata/golden_vectors.json")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python reference verifier failed: %v\n%s", err, out)
	}
}
