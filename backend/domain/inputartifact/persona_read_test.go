package inputartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

func personaEnvelope(t *testing.T, v ValidatedPersonaSpec) []byte {
	t.Helper()
	return encode(t, Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: v.CanonicalBytes()})
}
func TestPersonaStrictReadback(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		t.Run(role, func(t *testing.T) {
			v := personaValid(t, role)
			opts := personaOptions(t)
			if _, err := ReadPersonaSpec(v.CanonicalBytes(), v.ArtifactID(), opts); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadPersonaSpecEnvelope(personaEnvelope(t, v), v.ArtifactID(), opts); err != nil {
				t.Fatal(err)
			}
			for _, raw := range [][]byte{read(t, "testdata/persona_"+role+".json"), append(v.CanonicalBytes(), ' ')} {
				if _, err := ReadPersonaSpec(raw, v.ArtifactID(), opts); err == nil {
					t.Fatal("noncanonical stored bytes repaired")
				}
			}
			for _, id := range []string{"", "pspc_" + strings.Repeat("f", 64), "dprf_" + v.Digest()} {
				if _, err := ReadPersonaSpec(v.CanonicalBytes(), id, opts); err == nil {
					t.Fatal("wrong/missing expected identity accepted")
				}
			}
			for _, tc := range []struct {
				name string
				edit func(map[string]any)
			}{
				{"digest", func(e map[string]any) { e["digest"] = strings.Repeat("f", 64) }},
				{"profile", func(e map[string]any) { e["canonical_profile"] = "magi-cj/2" }},
				{"alg", func(e map[string]any) { e["digest_alg"] = "sha512" }},
				{"id", func(e map[string]any) { e["artifact_id"] = "pspc_" + strings.Repeat("f", 64) }},
				{"extra", func(e map[string]any) { e["approved"] = true }},
				{"missing", func(e map[string]any) { delete(e, "digest") }},
				{"null", func(e map[string]any) { e["body"] = nil }},
				{"string", func(e map[string]any) { e["body"] = string(v.CanonicalBytes()) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var e map[string]any
					_ = json.Unmarshal(personaEnvelope(t, v), &e)
					tc.edit(e)
					if _, err := ReadPersonaSpecEnvelope(encode(t, e), v.ArtifactID(), opts); err == nil {
						t.Fatal("invalid envelope accepted")
					}
				})
			}
			// Preserve the nested original whitespace: encoding/json.Marshal would
			// compact RawMessage and could accidentally repair a stored representation.
			raw := bytes.Replace(personaEnvelope(t, v), v.CanonicalBytes(), append([]byte(" "), v.CanonicalBytes()...), 1)
			// Whitespace immediately before a value belongs to the envelope transport.
			if _, err := ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts); err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(personaEnvelope(t, v), v.CanonicalBytes(), append([]byte("{ "), v.CanonicalBytes()[1:]...), 1)
			if _, err := ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts); err == nil {
				t.Fatal("noncanonical original nested body accepted")
			}
			p := personaFixture(t, role)
			p["version"] = "1.0.1"
			if _, err := ValidatePersonaSpec(encode(t, p), v.ArtifactID(), opts); err == nil {
				t.Fatal("changed content reused old ID")
			}
			c, err := canonical.Canonicalize(encode(t, p))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ReadPersonaSpec(c.Bytes(), v.ArtifactID(), opts); err == nil {
				t.Fatal("stored changed content reused old ID")
			}
			p = personaFixture(t, role)
			p["core_priority"].([]any)[0] = "UNRELATED"
			c, err = canonical.Canonicalize(encode(t, p))
			if err != nil {
				t.Fatal(err)
			}
			id, _ := c.ArtifactID(canonical.TypePersonaSpec)
			if _, err = ReadPersonaSpec(c.Bytes(), id, opts); err == nil {
				t.Fatal("matching identity bypassed stored semantics")
			}
		})
	}
}
func TestPersonaStrictParserAndType(t *testing.T) {
	v := personaValid(t, "melchior")
	raw := v.CanonicalBytes()
	for name, b := range map[string][]byte{
		"fence":         append([]byte("```json\n"), raw...),
		"duplicate":     bytes.Replace(raw, []byte(`"artifact_type":`), []byte(`"artifact_type":"persona_spec","artifact_type":`), 1),
		"float":         bytes.Replace(raw, []byte(`"tool_call_limit":6`), []byte(`"tool_call_limit":6.0`), 1),
		"exponent":      bytes.Replace(raw, []byte(`"tool_call_limit":6`), []byte(`"tool_call_limit":6e0`), 1),
		"negative zero": bytes.Replace(raw, []byte(`"tool_call_limit":6`), []byte(`"tool_call_limit":-0`), 1),
		"not nfc":       bytes.Replace(raw, []byte(`"description":"`), []byte("\"description\":\"e\u0301"), 1),
		"surrogate":     bytes.Replace(raw, []byte(`"description":"`), []byte(`"description":"\ud800`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidatePersonaSpec(b, "", personaOptions(t))
			var ce *canonical.Error
			if !errors.As(err, &ce) {
				t.Fatalf("strict C0 must reject before Schema: %v", err)
			}
		})
	}
	d := validate(t, fixture(t))
	if _, err := ValidatePersonaSpec(d.CanonicalBytes(), "", PersonaValidationOptions{}); err == nil {
		t.Fatal("domain accepted as persona")
	}
	if _, err := ReadPersonaSpec(d.CanonicalBytes(), d.ArtifactID(), PersonaValidationOptions{}); err == nil {
		t.Fatal("readback accepted wrong type")
	}
}
func TestPersonaImmutableResult(t *testing.T) {
	opts := personaOptions(t)
	money := Money{MinorUnits: 100, Currency: "USD", MinorUnitExponent: 2}
	b := opts.DomainProfile.Body().Budget
	b.CostLimit = &money
	opts.AdditionalBudgets = []Budget{b}
	raw := read(t, "testdata/persona_casper.json")
	v, err := ValidatePersonaSpec(raw, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	original := v.CanonicalBytes()
	id := v.ArtifactID()
	digest := v.Digest()
	raw[0] = '!'
	money.MinorUnits = 999
	opts.AdditionalBudgets[0].RoleToolCalls = 100
	body := v.Body()
	body.CorePriority[0] = "OVERRIDE"
	body.DomainExtensions[0].MinimumInvestigation[0].AcceptableEvidenceRuleIDs[0] = "missing"
	body.CreatedBy.SubjectID = "fake"
	cb := v.CanonicalBytes()
	cb[0] = '!'
	deps := v.PendingDependencies()
	deps[0].Path = "fake"
	deps[0].Reference.ContentDigest = "fake"
	budget, ok := v.EffectiveBudget()
	if !ok {
		t.Fatal("budget missing")
	}
	budget.CostLimit.MinorUnits = 999
	after, _ := v.EffectiveBudget()
	if v.ArtifactID() != id || v.Digest() != digest || !bytes.Equal(v.CanonicalBytes(), original) || v.Body().CorePriority[0] == "OVERRIDE" || v.Body().DomainExtensions[0].MinimumInvestigation[0].AcceptableEvidenceRuleIDs[0] != "fact" || v.PendingDependencies()[0].Path == "fake" || after.CostLimit.MinorUnits != 100 {
		t.Fatal("validated result aliases external mutable state")
	}
	if (ValidatedPersonaSpec{}).Status().Baseline != "" || (ValidatedPersonaSpec{}).ArtifactID() != "" {
		t.Fatal("zero value asserted validation")
	}
}
func TestPersonaEnvelopeResourceLimits(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		t.Run(role, func(t *testing.T) {
			v := personaValid(t, role)
			body := v.CanonicalBytes()
			raw := personaEnvelope(t, v)
			opts := personaOptions(t)
			opts.Limits = ResourceLimits{MaxBytes: len(body)}
			if _, err := ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts); err != nil {
				t.Fatal("exact body byte boundary", err)
			}
			opts.EnvelopeLimits = ResourceLimits{MaxBytes: len(raw)}
			if _, err := ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts); err != nil {
				t.Fatal("exact outer byte boundary", err)
			}
			_, err := ReadPersonaSpecEnvelope(append(raw, ' '), v.ArtifactID(), opts)
			requirePayloadLimit(t, err, len(raw)+1)
			opts.Limits.MaxBytes--
			_, err = ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts)
			requirePayloadLimit(t, err, len(body))
			if _, err = ReadPersonaSpec(body, v.ArtifactID(), opts); err == nil {
				t.Fatal("overlimit direct body accepted")
			}
			if _, err = ValidatePersonaSpec(body, "", opts); err == nil {
				t.Fatal("overlimit validation accepted")
			}
			opts.Limits.MaxBytes = len(body)
			opts.EnvelopeLimits = ResourceLimits{}
			cap := len(body) + DefaultEnvelopeByteAllowance
			padded := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), cap-len(raw))...)
			if _, err = ReadPersonaSpecEnvelope(padded, v.ArtifactID(), opts); err != nil {
				t.Fatal(err)
			}
			_, err = ReadPersonaSpecEnvelope(append(padded, ' '), v.ArtifactID(), opts)
			requirePayloadLimit(t, err, cap+1)
			var x any
			_ = json.Unmarshal(body, &x)
			members, depth, longest := 0, 0, 0
			var visit func(any, int)
			visit = func(x any, level int) {
				depth = max(depth, level)
				switch y := x.(type) {
				case map[string]any:
					members += len(y)
					for k, c := range y {
						longest = max(longest, len(k))
						visit(c, level+1)
					}
				case []any:
					members += len(y)
					for _, c := range y {
						visit(c, level+1)
					}
				case string:
					longest = max(longest, len(y))
				}
			}
			visit(x, 1)
			opts.Limits = ResourceLimits{MaxDepth: depth, MaxMembers: members, MaxStringBytes: longest}
			if _, err = ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts); err != nil {
				t.Fatal("exact structural body boundaries", err)
			}
			for _, tc := range []struct {
				limits ResourceLimits
				kind   string
				outer  bool
			}{
				{ResourceLimits{MaxDepth: depth - 1}, canonical.KindTooDeep, false},
				{ResourceLimits{MaxMembers: members - 1}, canonical.KindTooManyMembers, false},
				{ResourceLimits{MaxStringBytes: longest - 1}, canonical.KindStringTooLong, false},
				{ResourceLimits{MaxDepth: depth}, canonical.KindTooDeep, true},
				{ResourceLimits{MaxMembers: members + 4}, canonical.KindTooManyMembers, true},
				{ResourceLimits{MaxStringBytes: 1}, canonical.KindStringTooLong, true},
			} {
				opts.Limits = ResourceLimits{}
				opts.EnvelopeLimits = ResourceLimits{}
				if tc.outer {
					opts.EnvelopeLimits = tc.limits
				} else {
					opts.Limits = tc.limits
					opts.EnvelopeLimits = canonical.DefaultLimits()
				}
				_, err = ReadPersonaSpecEnvelope(raw, v.ArtifactID(), opts)
				var ce *canonical.Error
				if !errors.As(err, &ce) || ce.Kind != tc.kind {
					t.Fatalf("want %s got %v", tc.kind, err)
				}
			}
			maxInt := int(^uint(0) >> 1)
			opts = personaOptions(t)
			opts.Limits = ResourceLimits{MaxBytes: maxInt, MaxDepth: maxInt, MaxMembers: maxInt}
			padded = append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), canonical.DefaultLimits().MaxBytes)...)
			if _, err = ReadPersonaSpecEnvelope(padded, v.ArtifactID(), opts); err != nil {
				t.Fatal("overflow in derived outer limits", err)
			}
		})
	}
}
