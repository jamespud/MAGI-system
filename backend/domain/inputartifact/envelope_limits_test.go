package inputartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

func TestDomainEnvelopeAcceptsBodyAtByteLimit(t *testing.T) {
	v := validate(t, fixture(t))
	body := v.CanonicalBytes()
	opts := ValidationOptions{Limits: ResourceLimits{MaxBytes: len(body)}}
	if _, err := ReadDomainProfile(body, v.ArtifactID(), opts); err != nil {
		t.Fatal(err)
	}
	raw := encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: body})
	got, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts)
	if err != nil {
		t.Fatalf("F36-01: body at %d-byte limit must remain readable inside a bounded envelope: %v", len(body), err)
	}
	if got.ArtifactID() != v.ArtifactID() {
		t.Fatal("wrapping changed identity")
	}
}

func requirePayloadLimit(t *testing.T, err error, inputBytes int) {
	t.Helper()
	var ce *canonical.Error
	if !errors.As(err, &ce) || ce.Kind != canonical.KindPayloadTooLarge || !strings.Contains(ce.Detail, "input is "+strconv.Itoa(inputBytes)+" bytes") {
		t.Fatalf("expected payload limit for %d-byte input, got %v", inputBytes, err)
	}
}

func TestDomainEnvelopeSeparatesByteLimits(t *testing.T) {
	v := validate(t, fixture(t))
	body := v.CanonicalBytes()
	raw := encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: body})
	t.Run("explicit envelope boundary", func(t *testing.T) {
		opts := ValidationOptions{Limits: ResourceLimits{MaxBytes: len(body)}, EnvelopeLimits: ResourceLimits{MaxBytes: len(raw)}}
		if _, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts); err != nil {
			t.Fatal(err)
		}
		_, err := ReadDomainProfileEnvelope(append(raw, ' '), v.ArtifactID(), opts)
		requirePayloadLimit(t, err, len(raw)+1)
	})
	t.Run("body boundary still enforced", func(t *testing.T) {
		opts := ValidationOptions{Limits: ResourceLimits{MaxBytes: len(body) - 1}, EnvelopeLimits: ResourceLimits{MaxBytes: len(raw)}}
		_, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts)
		requirePayloadLimit(t, err, len(body))
	})
	t.Run("default envelope allowance remains bounded", func(t *testing.T) {
		opts := ValidationOptions{Limits: ResourceLimits{MaxBytes: len(body)}}
		cap := len(body) + DefaultEnvelopeByteAllowance
		padded := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), cap-len(raw))...)
		if _, err := ReadDomainProfileEnvelope(padded, v.ArtifactID(), opts); err != nil {
			t.Fatal(err)
		}
		_, err := ReadDomainProfileEnvelope(append(padded, ' '), v.ArtifactID(), opts)
		requirePayloadLimit(t, err, cap+1)
	})
}

func TestDomainEnvelopePreservesStructuralBodyLimits(t *testing.T) {
	v := validate(t, fixture(t))
	body := v.CanonicalBytes()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	// Measure the body's actual depth and member count, independently of the
	// production limit derivation, to exercise both exact structural boundaries.
	var members, depth int
	var visit func(any, int)
	visit = func(value any, level int) {
		if level > depth {
			depth = level
		}
		switch x := value.(type) {
		case map[string]any:
			members += len(x)
			for _, child := range x {
				visit(child, level+1)
			}
		case []any:
			members += len(x)
			for _, child := range x {
				visit(child, level+1)
			}
		}
	}
	visit(value, 1)
	opts := ValidationOptions{Limits: ResourceLimits{MaxDepth: depth, MaxMembers: members}}
	if _, err := ReadDomainProfile(body, v.ArtifactID(), opts); err != nil {
		t.Fatal(err)
	}
	raw := encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: body})
	if _, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, kind string
		limits     ResourceLimits
	}{
		{"depth", canonical.KindTooDeep, ResourceLimits{MaxDepth: depth - 1}},
		{"members", canonical.KindTooManyMembers, ResourceLimits{MaxMembers: members - 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts.Limits = tc.limits
			opts.EnvelopeLimits = canonical.DefaultLimits()
			_, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts)
			var ce *canonical.Error
			if !errors.As(err, &ce) || ce.Kind != tc.kind {
				t.Fatalf("expected inner %s, got %v", tc.kind, err)
			}
		})
	}
	for _, tc := range []struct {
		name, kind string
		limits     ResourceLimits
	}{
		{"outer depth", canonical.KindTooDeep, ResourceLimits{MaxDepth: depth}},
		{"outer members", canonical.KindTooManyMembers, ResourceLimits{MaxMembers: members + 4}},
		{"outer string", canonical.KindStringTooLong, ResourceLimits{MaxStringBytes: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), ValidationOptions{EnvelopeLimits: tc.limits})
			var ce *canonical.Error
			if !errors.As(err, &ce) || ce.Kind != tc.kind {
				t.Fatalf("expected outer %s, got %v", tc.kind, err)
			}
		})
	}
}

func TestDomainEnvelopeLimitDerivationDoesNotOverflow(t *testing.T) {
	v := validate(t, fixture(t))
	raw := encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: v.CanonicalBytes()})
	maxInt := int(^uint(0) >> 1)
	opts := ValidationOptions{Limits: ResourceLimits{MaxBytes: maxInt, MaxDepth: maxInt, MaxMembers: maxInt}}
	// Above the C0 default payload cap: an overflowing derived limit would
	// silently fall back to that default instead of honoring the caller's cap.
	raw = append(raw, bytes.Repeat([]byte(" "), canonical.DefaultLimits().MaxBytes)...)
	if _, err := ReadDomainProfileEnvelope(raw, v.ArtifactID(), opts); err != nil {
		t.Fatal(err)
	}
}
