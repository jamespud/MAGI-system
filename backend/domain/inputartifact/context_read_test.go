package inputartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

func contextEnvelope(t *testing.T, v ValidatedContextManifest) []byte {
	t.Helper()
	return encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: v.CanonicalBytes()})
}
func TestContextReadback(t *testing.T) {
	v := contextValid(t)
	body := v.CanonicalBytes()
	opts := contextOptions(t, v.Body())
	got, err := ReadContextManifest(body, v.ArtifactID(), opts)
	if err != nil || got.ArtifactID() != v.ArtifactID() {
		t.Fatalf("Adopt: %v", err)
	}
	for _, tc := range []struct {
		name, id, kind string
		raw            []byte
	}{
		{"expected-id-required", "", "IDENTITY", body},
		{"wrong-prefix", "dprf_" + v.Digest(), "IDENTITY", body},
		{"wrong-digest", "ctxm_" + strings.Repeat("0", 64), "IDENTITY", body},
		{"noncanonical-leading-space", v.ArtifactID(), "CANONICAL", append([]byte(" "), body...)},
		{"noncanonical-key-spacing", v.ArtifactID(), "CANONICAL", bytes.Replace(body, []byte(`"round":3`), []byte(`"round": 3`), 1)},
		{"markdown-fence", v.ArtifactID(), "CANONICAL", append([]byte("```json\n"), body...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadContextManifest(tc.raw, tc.id, opts)
			contextError(t, got, err, tc.kind)
		})
	}
	raw := contextEnvelope(t, v)
	got, err = ReadContextManifestEnvelope(raw, v.ArtifactID(), opts)
	if err != nil || got.ArtifactID() != v.ArtifactID() {
		t.Fatalf("envelope: %v", err)
	}
	for _, tc := range []struct {
		name, kind string
		edit       func(map[string]any)
	}{
		{"wrapper-digest", "IDENTITY", func(e map[string]any) { e["digest"] = strings.Repeat("0", 64) }},
		{"wrapper-id", "ENVELOPE", func(e map[string]any) { e["artifact_id"] = "ctxm_" + strings.Repeat("0", 64) }},
		{"profile", "ENVELOPE", func(e map[string]any) { e["canonical_profile"] = "legacy-manifest/v1" }},
		{"algorithm", "ENVELOPE", func(e map[string]any) { e["digest_alg"] = "sha1" }},
		{"extra-field", "ENVELOPE", func(e map[string]any) { e["verified"] = true }},
		{"missing-field", "ENVELOPE", func(e map[string]any) { delete(e, "digest") }},
		{"null-field", "ENVELOPE", func(e map[string]any) { e["digest"] = nil }},
		{"null-body", "ENVELOPE", func(e map[string]any) { e["body"] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e map[string]any
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatal(err)
			}
			tc.edit(e)
			got, err := ReadContextManifestEnvelope(encode(t, e), v.ArtifactID(), opts)
			contextError(t, got, err, tc.kind)
		})
	}
	t.Run("nested-body-not-reencoded", func(t *testing.T) {
		bad := bytes.Replace(raw, []byte(`"round":3`), []byte(`"round": 3`), 1)
		got, err := ReadContextManifestEnvelope(bad, v.ArtifactID(), opts)
		contextError(t, got, err, "CANONICAL")
	})
	t.Run("duplicate-wrapper-field", func(t *testing.T) {
		bad := append([]byte(`{"digest":"`+v.Digest()+`",`), raw[1:]...)
		got, err := ReadContextManifestEnvelope(bad, v.ArtifactID(), opts)
		contextError(t, got, err, "CANONICAL")
	})
}

func TestContextEnvelopeResourceBoundaries(t *testing.T) {
	v := contextValid(t)
	body := v.CanonicalBytes()
	raw := contextEnvelope(t, v)
	opts := contextOptions(t, v.Body())
	opts.Limits.MaxBytes = len(body)
	// F36-01: wrapping an exact-cap canonical body must not consume body budget.
	if _, err := ReadContextManifestEnvelope(raw, v.ArtifactID(), opts); err != nil {
		t.Fatalf("exact body cap inside envelope: %v", err)
	}
	t.Run("outer-byte-cap", func(t *testing.T) {
		o := opts
		o.EnvelopeLimits.MaxBytes = len(raw)
		if _, err := ReadContextManifestEnvelope(raw, v.ArtifactID(), o); err != nil {
			t.Fatal(err)
		}
		_, err := ReadContextManifestEnvelope(append(raw, ' '), v.ArtifactID(), o)
		requirePayloadLimit(t, err, len(raw)+1)
	})
	t.Run("body-byte-cap-plus-one", func(t *testing.T) {
		o := opts
		o.Limits.MaxBytes = len(body) - 1
		o.EnvelopeLimits.MaxBytes = len(raw)
		_, err := ReadContextManifestEnvelope(raw, v.ArtifactID(), o)
		requirePayloadLimit(t, err, len(body))
	})
	t.Run("default-outer-ceiling", func(t *testing.T) {
		cap := len(body) + DefaultEnvelopeByteAllowance
		padded := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), cap-len(raw))...)
		if _, err := ReadContextManifestEnvelope(padded, v.ArtifactID(), opts); err != nil {
			t.Fatal(err)
		}
		_, err := ReadContextManifestEnvelope(append(padded, ' '), v.ArtifactID(), opts)
		requirePayloadLimit(t, err, cap+1)
	})
	// Measure the actual body, independently of production limit derivation.
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	depth, members := 0, 0
	var walk func(any, int)
	walk = func(v any, d int) {
		if d > depth {
			depth = d
		}
		switch x := v.(type) {
		case map[string]any:
			members += len(x)
			for _, child := range x {
				walk(child, d+1)
			}
		case []any:
			members += len(x)
			for _, child := range x {
				walk(child, d+1)
			}
		}
	}
	walk(value, 1)
	t.Run("exact-structural-limits", func(t *testing.T) {
		o := opts
		o.Limits.MaxDepth = depth
		o.Limits.MaxMembers = members
		if _, err := ReadContextManifestEnvelope(raw, v.ArtifactID(), o); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name, kind   string
		inner, outer ResourceLimits
	}{
		{"inner-depth", canonical.KindTooDeep, ResourceLimits{MaxDepth: depth - 1}, canonical.DefaultLimits()},
		{"inner-members", canonical.KindTooManyMembers, ResourceLimits{MaxMembers: members - 1}, canonical.DefaultLimits()},
		{"inner-string", canonical.KindStringTooLong, ResourceLimits{MaxStringBytes: 1}, canonical.DefaultLimits()},
		{"outer-depth", canonical.KindTooDeep, ResourceLimits{}, ResourceLimits{MaxDepth: depth}},
		{"outer-members", canonical.KindTooManyMembers, ResourceLimits{}, ResourceLimits{MaxMembers: members + 4}},
		{"outer-string", canonical.KindStringTooLong, ResourceLimits{}, ResourceLimits{MaxStringBytes: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := opts
			o.Limits = tc.inner
			o.EnvelopeLimits = tc.outer
			_, err := ReadContextManifestEnvelope(raw, v.ArtifactID(), o)
			var ce *canonical.Error
			if !errors.As(err, &ce) || ce.Kind != tc.kind {
				t.Fatalf("expected %s, got %v", tc.kind, err)
			}
		})
	}
	t.Run("overflow-safe-wrapper-allowance", func(t *testing.T) {
		o := opts
		maxInt := int(^uint(0) >> 1)
		o.Limits = ResourceLimits{MaxBytes: maxInt, MaxDepth: maxInt, MaxMembers: maxInt}
		padded := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), canonical.DefaultLimits().MaxBytes)...)
		if _, err := ReadContextManifestEnvelope(padded, v.ArtifactID(), o); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("context-default-byte-cap", func(t *testing.T) {
		o := ContextValidationOptions{}
		padded := append(append([]byte(nil), body...), bytes.Repeat([]byte(" "), 262145-len(body))...)
		_, err := ReadContextManifest(padded, v.ArtifactID(), o)
		requirePayloadLimit(t, err, 262145)
	})
}

func TestContextRawSnapshotLimits(t *testing.T) {
	m := contextFixture(t)
	opts := contextOptions(t, m)
	maxLen, total := 0, 0
	for _, s := range opts.Snapshots {
		total += len(s.RawBytes)
		if len(s.RawBytes) > maxLen {
			maxLen = len(s.RawBytes)
		}
	}
	opts.MaxSnapshotBytes = maxLen
	opts.MaxTotalSnapshotBytes = total
	if _, err := ValidateContextManifest(encode(t, m), "", opts); err != nil {
		t.Fatalf("exact independent blob cap: %v", err)
	}
	for _, which := range []string{"blob", "total"} {
		t.Run(which, func(t *testing.T) {
			o := opts
			if which == "blob" {
				o.MaxSnapshotBytes--
			} else {
				o.MaxTotalSnapshotBytes--
			}
			v, err := ValidateContextManifest(encode(t, m), "", o)
			contextError(t, v, err, "RESOURCE_LIMIT")
		})
	}
}
