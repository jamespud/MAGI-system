package inputartifact

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

var contextRoles = []string{"melchior", "balthasar", "casper"}

func contextFixture(t *testing.T) ContextManifest {
	t.Helper()
	var m ContextManifest
	if err := json.Unmarshal(read(t, "testdata/context_manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func contextOptions(t *testing.T, m ContextManifest) ContextValidationOptions {
	t.Helper()
	d := validate(t, fixture(t))
	opts := ContextValidationOptions{DomainProfile: &d, Personas: map[string]ValidatedPersonaSpec{}, Snapshots: map[string]ContextSnapshot{}}
	roles := map[string]RoleView{}
	for _, role := range contextRoles {
		opts.Personas[role] = personaValid(t, role)
		v := m.Roles[role]
		v.AllowedContextIDs = append([]string{}, v.AllowedContextIDs...)
		roles[role] = v
	}
	opts.Selection = &ContextSelection{
		CaseID: m.CaseID, Round: m.Round, Charter: ContextCharterBinding{ArtifactID: m.CharterID, SchemaVersion: "2.0", Version: "2.0.0", DomainProfileID: m.DomainProfileID, Validity: m.Validity, ValidityHorizon: "2026-10-23T00:00:00.000Z"},
		DomainProfileVersion: d.Body().Version, Roles: roles, SharedFactSnapshotID: m.SharedFactSnapshotID, UserPreferenceSnapshotID: m.UserPreferenceSnapshotID, SourceDependencyIndexVersion: m.SourceDependencyIndexVersion,
		ContextRevocationScopeID: m.ContextRevocationScopeID, ContextRevocationEpoch: m.ContextRevocationEpoch, AdmissionPolicyRef: m.AdmissionPolicyRef,
	}
	var materials map[string]string
	if err := json.Unmarshal(read(t, "testdata/context_snapshot_bytes.json"), &materials); err != nil {
		t.Fatal(err)
	}
	for _, s := range m.SnapshotRefs {
		raw, err := hex.DecodeString(materials[s.SnapshotID])
		if err != nil {
			t.Fatal(err)
		}
		// Descriptors and option pointers must be independent of body mutation.
		var descriptor SnapshotRef
		if err = json.Unmarshal(encode(t, s), &descriptor); err != nil {
			t.Fatal(err)
		}
		opts.Snapshots[s.SnapshotID] = ContextSnapshot{Descriptor: descriptor, RawBytes: raw}
	}
	opts.UsageWindow = &Validity{NotBefore: "2026-10-22T01:00:00.000Z", ValidUntil: strptr("2026-10-22T02:00:00.000Z")}
	return opts
}
func strptr(s string) *string { return &s }

// Fully checked local relations use separately validated, extension-free
// Personas. The frozen Golden Personas retain their unresolved extensions.
func contextCompleteOptions(t *testing.T, m *ContextManifest) ContextValidationOptions {
	t.Helper()
	opts := contextOptions(t, *m)
	for _, role := range contextRoles {
		body := personaFixture(t, role)
		body["domain_extensions"] = []any{}
		p, err := ValidatePersonaSpec(encode(t, body), "", personaOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		if p.Status().DomainRelations != "CHECKED_OFFLINE" {
			t.Fatalf("%s relations not fully checked: %+v", role, p.Status())
		}
		opts.Personas[role] = p
		v := m.Roles[role]
		v.PersonaSpecID = p.ArtifactID()
		m.Roles[role] = v
		selected := opts.Selection.Roles[role]
		selected.PersonaSpecID = p.ArtifactID()
		opts.Selection.Roles[role] = selected
	}
	return opts
}

// F41-02: exact identity, role and supplied domain bytes cannot upgrade a
// Persona's unresolved extension relations into complete Context bindings.
func TestContextPartialPersonaDomainRelations(t *testing.T) {
	t.Run("all-checked", func(t *testing.T) {
		m := contextFixture(t)
		opts := contextCompleteOptions(t, &m)
		opts.RequireCompleteBindings = true
		v, err := ValidateContextManifest(encode(t, m), "", opts)
		if err != nil {
			t.Fatal(err)
		}
		want := ContextValidationStatus{Structure: "VALID_OFFLINE", Bindings: "CHECKED_OFFLINE", SnapshotBytes: "CHECKED_OFFLINE", Qualification: "UNVERIFIABLE_REFERENCE", ActualReads: "NOT_RUN"}
		if v.Status() != want || len(v.PendingDependencies()) == 0 {
			t.Fatalf("complete local relations changed qualification/reads: %+v", v.Status())
		}
	})
	for _, role := range contextRoles {
		t.Run(role, func(t *testing.T) {
			m := contextFixture(t)
			opts := contextCompleteOptions(t, &m)
			p := personaValid(t, role)
			if p.Status().DomainRelations != "PARTIAL_OFFLINE" || p.DomainProfileID() != opts.DomainProfile.ArtifactID() || p.Body().PersonaID != role {
				t.Fatal("regression requires a partially checked Persona with correct domain and role")
			}
			opts.Personas[role] = p
			view := m.Roles[role]
			view.PersonaSpecID = p.ArtifactID()
			m.Roles[role] = view
			selected := opts.Selection.Roles[role]
			selected.PersonaSpecID = p.ArtifactID()
			opts.Selection.Roles[role] = selected
			contextSchemaValid(t, m)
			t.Run("partial", func(t *testing.T) {
				v, err := ValidateContextManifest(encode(t, m), "", opts)
				if err != nil {
					t.Fatal(err)
				}
				want := ContextValidationStatus{Structure: "VALID_OFFLINE", Bindings: "PARTIAL_OFFLINE", SnapshotBytes: "CHECKED_OFFLINE", Qualification: "UNVERIFIABLE_REFERENCE", ActualReads: "NOT_RUN"}
				if v.Status() != want {
					t.Errorf("partial Persona domain relations promoted: %+v", v.Status())
				}
				found := false
				for _, dep := range v.PendingDependencies() {
					if dep.Kind == "UNVERIFIABLE_REQUIREMENT_BINDING" && dep.Path == "$.roles."+role+".persona.domain_extensions[0].minimum_investigation[0]" {
						found = true
					}
				}
				if !found {
					t.Fatal("unresolved Persona extension dependency lost")
				}
			})
			t.Run("strict", func(t *testing.T) {
				opts.RequireCompleteBindings = true
				v, err := ValidateContextManifest(encode(t, m), "", opts)
				contextError(t, v, err, "UNVERIFIABLE_REFERENCE")
			})
		})
	}
}

func contextValid(t *testing.T) ValidatedContextManifest {
	t.Helper()
	m := contextFixture(t)
	v, err := ValidateContextManifest(encode(t, m), "", contextOptions(t, m))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func contextError(t *testing.T, v ValidatedContextManifest, err error, kind string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Kind != kind {
		t.Fatalf("expected %s, got %v", kind, err)
	}
	if v.ArtifactID() != "" || v.Digest() != "" || len(v.CanonicalBytes()) != 0 || !reflect.DeepEqual(v.Status(), ContextValidationStatus{}) {
		t.Fatal("rejection returned validation evidence")
	}
}
func contextSchemaValid(t *testing.T, m ContextManifest) {
	t.Helper()
	res := schemaValidator.Validate(contractSchema, encode(t, m))
	if !res.Valid {
		t.Fatalf("semantic mutation broke schema: %+v", res.Violations)
	}
}
func TestContextGolden(t *testing.T) {
	for _, name := range []string{"context_manifest", "context_manifest_empty_views"} {
		t.Run(name, func(t *testing.T) {
			var m ContextManifest
			if err := json.Unmarshal(read(t, "testdata/"+name+".json"), &m); err != nil {
				t.Fatal(err)
			}
			opts := contextOptions(t, m)
			v, err := ValidateContextManifest(read(t, "testdata/"+name+".json"), "", opts)
			if err != nil {
				t.Fatal(err)
			}
			var g struct {
				ID     string `json:"artifact_id"`
				Digest string `json:"digest"`
			}
			if err = json.Unmarshal(read(t, "testdata/"+name+".golden.json"), &g); err != nil {
				t.Fatal(err)
			}
			if v.ArtifactID() != g.ID || v.Digest() != g.Digest || !bytes.Equal(v.CanonicalBytes(), read(t, "testdata/"+name+".canonical.json")) {
				t.Fatal("independent Python golden mismatch")
			}
			want := ContextValidationStatus{Structure: "VALID_OFFLINE", Bindings: "PARTIAL_OFFLINE", SnapshotBytes: "CHECKED_OFFLINE", Qualification: "UNVERIFIABLE_REFERENCE", ActualReads: "NOT_RUN"}
			if v.Status() != want {
				t.Fatalf("status: %+v", v.Status())
			}
			if len(v.PendingDependencies()) == 0 {
				t.Fatal("offline identity erased qualification dependencies")
			}
			opts.RequireCompleteBindings = true
			v, err = ValidateContextManifest(read(t, "testdata/"+name+".json"), "", opts)
			contextError(t, v, err, "UNVERIFIABLE_REFERENCE")
		})
	}
}

// Each case remains valid under the frozen Schema. Omitting the named semantic
// check must make its mutation succeed. No malformed IDs mask the interleaving.
func TestContextSemanticMutations(t *testing.T) {
	cases := []struct {
		name, kind string
		edit       func(*ContextManifest, *ContextValidationOptions)
	}{
		{"duplicate-snapshot-id-different-content", "COLLECTION", func(m *ContextManifest, o *ContextValidationOptions) {
			s := m.SnapshotRefs[0]
			s.ContentDigest = strings.Repeat("a", 64)
			m.SnapshotRefs = append(m.SnapshotRefs[:1], append([]SnapshotRef{s}, m.SnapshotRefs[1:]...)...)
		}},
		{"unordered-snapshot-refs", "COLLECTION", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0], m.SnapshotRefs[1] = m.SnapshotRefs[1], m.SnapshotRefs[0]
		}},
		{"missing-snapshot", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) { m.SnapshotRefs = m.SnapshotRefs[1:] }},
		{"orphan-snapshot", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) {
			s := m.SnapshotRefs[0]
			s.SnapshotID = "zz-orphan-1"
			m.SnapshotRefs = append(m.SnapshotRefs, s)
		}},
		{"wrong-snapshot-kind", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) { m.SnapshotRefs[0].Kind = "SHARED_FACT" }},
		{"false-index-binding", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SourceDependencyIndexVersion = m.SharedFactSnapshotID
		}},
		{"snapshot-version-latest", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { m.SnapshotRefs[0].Version = "latest" }},
		{"snapshot-version-namespace-alias", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { m.SnapshotRefs[0].Version = "release/latest" }},
		{"snapshot-id-alias-consistent", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) {
			old := m.SharedFactSnapshotID
			m.SharedFactSnapshotID = "facts/latest"
			for i := range m.SnapshotRefs {
				if m.SnapshotRefs[i].SnapshotID == old {
					m.SnapshotRefs[i].SnapshotID = m.SharedFactSnapshotID
				}
			}
			sort.Slice(m.SnapshotRefs, func(i, j int) bool { return m.SnapshotRefs[i].SnapshotID < m.SnapshotRefs[j].SnapshotID })
			o.Selection = nil
			o.Snapshots = nil
		}},
		{"nested-source-version-alias", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].SourceRef.Version = "release/CURRENT"
		}},
		{"nested-policy-version-alias", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].QualificationPolicyRef.Version = "stable"
		}},
		{"identity-source-alias", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { m.CreatedBy.IdentitySourceVersion = "HEAD" }},
		{"admission-version-alias", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { m.AdmissionPolicyRef.Version = "active" }},
		{"same-source-version-digest-conflict", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[1].SourceRef = m.SnapshotRefs[0].SourceRef
			m.SnapshotRefs[1].SourceRef.ContentDigest = strings.Repeat("b", 64)
		}},
		{"allowed-contexts-unordered", "COLLECTION", func(m *ContextManifest, o *ContextValidationOptions) {
			v := m.Roles["melchior"]
			v.AllowedContextIDs[0], v.AllowedContextIDs[1] = v.AllowedContextIDs[1], v.AllowedContextIDs[0]
			m.Roles["melchior"] = v
		}},
		{"cross-role-experience-inheritance", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) {
			v := m.Roles["casper"]
			v.ExperienceSnapshotID = m.Roles["melchior"].ExperienceSnapshotID
			m.Roles["casper"] = v
		}},
		{"cross-role-experience-swap", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			a, b := m.Roles["melchior"], m.Roles["casper"]
			a.ExperienceSnapshotID, b.ExperienceSnapshotID = b.ExperienceSnapshotID, a.ExperienceSnapshotID
			m.Roles["melchior"], m.Roles["casper"] = a, b
		}},
		{"cross-role-persona-swap", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			a, b := m.Roles["melchior"], m.Roles["casper"]
			a.PersonaSpecID, b.PersonaSpecID = b.PersonaSpecID, a.PersonaSpecID
			m.Roles["melchior"], m.Roles["casper"] = a, b
		}},
		{"persona-role-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection = nil
			o.Personas["melchior"] = o.Personas["casper"]
		}},
		{"duplicate-persona-view", "REFERENCE", func(m *ContextManifest, o *ContextValidationOptions) {
			v := m.Roles["casper"]
			v.PersonaSpecID = m.Roles["melchior"].PersonaSpecID
			m.Roles["casper"] = v
		}},
		{"allowed-context-expansion-with-descriptor", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			v := m.Roles["melchior"]
			v.AllowedContextIDs = append(v.AllowedContextIDs, "zz-new-context")
			m.Roles["melchior"] = v
			s := m.SnapshotRefs[0]
			s.SnapshotID = "zz-new-context"
			s.Kind = "CONTEXT"
			m.SnapshotRefs = append(m.SnapshotRefs, s)
		}},
		{"charter-id-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.CharterID = "chtr_" + strings.Repeat("e", 64) }},
		{"charter-domain-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection.Charter.DomainProfileID = "dprf_" + strings.Repeat("e", 64)
		}},
		{"domain-id-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			m.DomainProfileID = "dprf_" + strings.Repeat("e", 64)
			o.Selection = nil
		}},
		{"domain-version-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { o.Selection.DomainProfileVersion = "9.9.9" }},
		{"wrong-case", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.CaseID = "other-case" }},
		{"wrong-round", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.Round++ }},
		{"stale-scope", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.ContextRevocationScopeID = "other-scope" }},
		{"stale-epoch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.ContextRevocationEpoch-- }},
		{"admission-policy-substitution", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.AdmissionPolicyRef.ObjectID = "other-policy" }},
		{"shared-fact-substitution", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection.SharedFactSnapshotID = "other-facts"
		}},
		{"preference-substitution", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection.UserPreferenceSnapshotID = "other-preferences"
		}},
		{"snapshot-version-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { m.SnapshotRefs[0].Version = "9" }},
		{"snapshot-raw-digest-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].ContentDigest = strings.Repeat("e", 64)
		}},
		{"snapshot-ref-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].SourceRef.ObjectID = "other-source"
		}},
		{"snapshot-validity-mismatch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].Validity.NotBefore = "2026-10-22T00:00:01.000Z"
		}},
		{"tampered-raw-bytes", "IDENTITY", func(m *ContextManifest, o *ContextValidationOptions) {
			s := o.Snapshots[m.SnapshotRefs[0].SnapshotID]
			s.RawBytes = []byte("tampered")
			o.Snapshots[m.SnapshotRefs[0].SnapshotID] = s
		}},
		{"unknown-material", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { o.Snapshots["orphan"] = ContextSnapshot{} }},
		{"unknown-persona-context", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { o.Personas["other"] = o.Personas["casper"] }},
		{"zero-persona-context", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { o.Personas["casper"] = ValidatedPersonaSpec{} }},
		{"zero-domain-context", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) { o.DomainProfile = &ValidatedDomainProfile{} }},
		{"invalid-calendar", "TIME", func(m *ContextManifest, o *ContextValidationOptions) { m.FrozenAt = "2026-02-30T00:00:00.000Z" }},
		{"empty-manifest-validity", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			m.Validity.ValidUntil = strptr(m.Validity.NotBefore)
		}},
		{"empty-snapshot-validity", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].Validity.ValidUntil = strptr(m.SnapshotRefs[0].Validity.NotBefore)
		}},
		{"expired-snapshot-at-freeze", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].Validity.ValidUntil = strptr("2026-10-21T00:00:00.000Z")
		}},
		{"source-outside-manifest-window", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].Validity = Validity{NotBefore: "2026-10-24T00:00:00.000Z", ValidUntil: nil}
		}},
		{"source-expires-before-usage-end", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			m.SnapshotRefs[0].Validity.ValidUntil = strptr("2026-10-22T01:30:00.000Z")
			s := o.Snapshots[m.SnapshotRefs[0].SnapshotID]
			s.Descriptor.Validity = m.SnapshotRefs[0].Validity
			o.Snapshots[m.SnapshotRefs[0].SnapshotID] = s
		}},
		{"charter-horizon-excludes-usage", "TIME", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection.Charter.ValidityHorizon = "2026-10-22T01:30:00.000Z"
		}},
		{"unknown-charter-schema", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { o.Selection.Charter.SchemaVersion = "3.0" }},
		{"mutable-charter-version", "VERSION", func(m *ContextManifest, o *ContextValidationOptions) { o.Selection.Charter.Version = "latest" }},
		{"invalid-selected-epoch", "BINDING", func(m *ContextManifest, o *ContextValidationOptions) {
			o.Selection.ContextRevocationEpoch = canonical.MaxSafeInteger + 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := contextFixture(t)
			opts := contextOptions(t, m)
			tc.edit(&m, &opts)
			contextSchemaValid(t, m)
			v, err := ValidateContextManifest(encode(t, m), "", opts)
			contextError(t, v, err, tc.kind)
		})
	}
}

func TestContextFrozenSchemaAndC0(t *testing.T) {
	cases := []struct {
		name, kind string
		edit       func(map[string]any)
	}{
		{"wrong-role-key", "SCHEMA", func(m map[string]any) {
			r := m["roles"].(map[string]any)
			r["other"] = r["casper"]
			delete(r, "casper")
		}},
		{"missing-role", "SCHEMA", func(m map[string]any) { delete(m["roles"].(map[string]any), "casper") }},
		{"extra-role", "SCHEMA", func(m map[string]any) { r := m["roles"].(map[string]any); r["other"] = r["casper"] }},
		{"duplicate-context", "SCHEMA", func(m map[string]any) {
			r := m["roles"].(map[string]any)["casper"].(map[string]any)
			a := r["allowed_context_ids"].([]any)
			r["allowed_context_ids"] = append(a, a[0])
		}},
		{"duplicate-identical-descriptor", "SCHEMA", func(m map[string]any) { a := m["snapshot_refs"].([]any); m["snapshot_refs"] = append(a, a[0]) }},
		{"wrong-round-zero", "SCHEMA", func(m map[string]any) { m["round"] = 0 }},
		{"round-overflow", "CANONICAL", func(m map[string]any) { m["round"] = int64(9007199254740992) }},
		{"missing-scope", "SCHEMA", func(m map[string]any) { delete(m, "context_revocation_scope_id") }},
		{"missing-epoch", "SCHEMA", func(m map[string]any) { delete(m, "context_revocation_epoch") }},
		{"self-digest", "SCHEMA", func(m map[string]any) { m["digest"] = strings.Repeat("0", 64) }},
		{"unknown-schema", "SCHEMA", func(m map[string]any) { m["schema_version"] = "2.0" }},
		{"null-body-field", "SCHEMA", func(m map[string]any) { m["roles"] = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(read(t, "testdata/context_manifest.json"), &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(m)
			v, err := ValidateContextManifest(encode(t, m), "", ContextValidationOptions{})
			contextError(t, v, err, tc.kind)
		})
	}
	raw := read(t, "testdata/context_manifest.canonical.json")
	raw = bytes.Replace(raw, []byte(`"round":3`), []byte(`"round":3,"round":3`), 1)
	v, err := ValidateContextManifest(raw, "", ContextValidationOptions{})
	contextError(t, v, err, "CANONICAL")
}

func TestContextQualificationAndDependencies(t *testing.T) {
	for _, complete := range []bool{false, true} {
		m := contextFixture(t)
		opts := ContextValidationOptions{}
		if complete {
			opts = contextCompleteOptions(t, &m)
		}
		v, err := ValidateContextManifest(encode(t, m), "", opts)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status().Qualification != "UNVERIFIABLE_REFERENCE" || v.Status().ActualReads != "NOT_RUN" {
			t.Fatal("offline result promoted authority/reads")
		}
		for _, path := range []string{"$.charter_id", "$.user_preference_snapshot_id", "$.admission_policy_ref", "$.created_by", "$.context_revocation_scope_id", "$.case_id"} {
			found := false
			for _, dep := range v.PendingDependencies() {
				if dep.Path == path {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing unverified obligation %s", path)
			}
		}
		opts.RequireCompleteQualification = true
		v, err = ValidateContextManifest(encode(t, m), "", opts)
		contextError(t, v, err, "UNVERIFIABLE_REFERENCE")
	}
	for _, field := range []string{"selection", "domain", "personas", "snapshots", "snapshot-bytes"} {
		t.Run(field, func(t *testing.T) {
			m := contextFixture(t)
			opts := contextCompleteOptions(t, &m)
			opts.RequireCompleteBindings = true
			switch field {
			case "selection":
				opts.Selection = nil
			case "domain":
				opts.DomainProfile = nil
			case "personas":
				delete(opts.Personas, "casper")
			case "snapshots":
				delete(opts.Snapshots, m.SnapshotRefs[0].SnapshotID)
			case "snapshot-bytes":
				s := opts.Snapshots[m.SnapshotRefs[0].SnapshotID]
				s.RawBytes = nil
				opts.Snapshots[m.SnapshotRefs[0].SnapshotID] = s
			}
			v, err := ValidateContextManifest(encode(t, m), "", opts)
			contextError(t, v, err, "UNVERIFIABLE_REFERENCE")
			opts.RequireCompleteBindings = false
			v, err = ValidateContextManifest(encode(t, m), "", opts)
			if err != nil {
				t.Fatal(err)
			}
			if v.Status().Bindings == "CHECKED_OFFLINE" && v.Status().SnapshotBytes == "CHECKED_OFFLINE" {
				t.Fatal("incomplete evidence claimed complete")
			}
		})
	}
	// A SnapshotRef carries its own digest; walking it as an ExternalRef would
	// skip these nested policies and silently lose their qualification states.
	v := contextValid(t)
	for _, path := range []string{"$.snapshot_refs[0].source_ref", "$.snapshot_refs[0].qualification_policy_ref"} {
		found := false
		for _, d := range v.PendingDependencies() {
			if d.Path == path && d.Reference.Namespace != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing nested ref %s", path)
		}
	}
}

func TestContextIdentityAndDefensiveCopies(t *testing.T) {
	m := contextFixture(t)
	opts := contextOptions(t, m)
	v := contextValid(t)
	body := v.CanonicalBytes()
	deps := v.PendingDependencies()
	status := v.Status()
	formatted := append([]byte(" \n"), encode(t, m)...)
	equivalent, err := ValidateContextManifest(formatted, v.ArtifactID(), opts)
	if err != nil || !bytes.Equal(body, equivalent.CanonicalBytes()) {
		t.Fatalf("equivalence: %v", err)
	}
	m.Version = "1.0.1"
	changed, err := ValidateContextManifest(encode(t, m), "", opts)
	if err != nil || changed.ArtifactID() == v.ArtifactID() {
		t.Fatalf("content change: %v", err)
	}
	got, err := ValidateContextManifest(encode(t, m), v.ArtifactID(), opts)
	contextError(t, got, err, "IDENTITY")
	raw := v.CanonicalBytes()
	raw[0] = 'x'
	b := v.Body()
	b.Roles["melchior"].AllowedContextIDs[0] = "tampered"
	b.SnapshotRefs[0].Validity.NotBefore = "tampered"
	b.SnapshotRefs[0].SourceRef.ObjectID = "tampered"
	pd := v.PendingDependencies()
	if len(pd) == 0 {
		t.Fatal("missing unresolved dependencies")
	}
	pd[0].Kind = "VERIFIED"
	pd[0].Reference.ObjectID = "tampered"
	opts.Selection.Roles["melchior"].AllowedContextIDs[0] = "tampered"
	opts.Snapshots[m.SnapshotRefs[0].SnapshotID].RawBytes[0] = 'x'
	if !bytes.Equal(body, v.CanonicalBytes()) || !reflect.DeepEqual(deps, v.PendingDependencies()) || status != v.Status() {
		t.Fatal("caller mutated validated result")
	}
	if reflect.DeepEqual(b, v.Body()) {
		t.Fatal("body shares nested storage")
	}
	var zero ValidatedContextManifest
	if zero.ArtifactID() != "" || len(zero.PendingDependencies()) != 0 || zero.Status() != (ContextValidationStatus{}) {
		t.Fatal("zero value claimed validation")
	}
}

func TestContextDeterminism(t *testing.T) {
	m := contextFixture(t)
	opts := contextOptions(t, m)
	a, err := ValidateContextManifest(encode(t, m), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		b, e := ValidateContextManifest(encode(t, m), "", opts)
		if e != nil || !reflect.DeepEqual(a.PendingDependencies(), b.PendingDependencies()) || a.Status() != b.Status() {
			t.Fatal("nondeterministic offline result")
		}
	}
}

func TestContextRuntimeIntegration(t *testing.T) {
	t.Skip("NOT RUN: actual injection, model reads, delivery receipts and Evidence Closure are C1-C2")
}
func TestContextAuthorityIntegration(t *testing.T) {
	t.Skip("BLOCKED: Charter approval, source/disclosure/permission qualification and online scope/epoch authority require C2/C3")
}

func TestContextPersonaDomainEvidenceCannotBeInferredFromSelectedID(t *testing.T) {
	m := contextFixture(t)
	opts := contextCompleteOptions(t, &m)
	// The Persona's selected ID can be present even though Persona validation
	// never received domain bytes. An ID assertion is not a checked relation.
	p, err := ValidatePersonaSpec(read(t, "testdata/persona_casper.json"), "", PersonaValidationOptions{DomainProfileID: m.DomainProfileID})
	if err != nil {
		t.Fatal(err)
	}
	opts.Personas["casper"] = p
	view := m.Roles["casper"]
	view.PersonaSpecID = p.ArtifactID()
	m.Roles["casper"] = view
	selected := opts.Selection.Roles["casper"]
	selected.PersonaSpecID = p.ArtifactID()
	opts.Selection.Roles["casper"] = selected
	opts.RequireCompleteBindings = true
	v, err := ValidateContextManifest(encode(t, m), "", opts)
	contextError(t, v, err, "UNVERIFIABLE_REFERENCE")
	opts.RequireCompleteBindings = false
	v, err = ValidateContextManifest(encode(t, m), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status().Bindings == "CHECKED_OFFLINE" {
		t.Fatal("unverified Persona/domain relation promoted by a selected ID")
	}
}

func TestContextNoCommonSourceInterval(t *testing.T) {
	m := contextFixture(t)
	m.SnapshotRefs[0].Validity.ValidUntil = strptr("2026-10-22T01:00:00.000Z")
	m.SnapshotRefs[1].Validity.NotBefore = "2026-10-22T02:00:00.000Z"
	// Both intersect the Manifest independently, but no common initial view exists.
	v, err := ValidateContextManifest(encode(t, m), "", ContextValidationOptions{})
	contextError(t, v, err, "TIME")
}

func TestContextCrossArtifactReferenceConflict(t *testing.T) {
	m := contextFixture(t)
	opts := contextOptions(t, m)
	m.IssuancePolicyRef = opts.DomainProfile.Body().IssuancePolicyRef
	m.IssuancePolicyRef.ContentDigest = strings.Repeat("e", 64)
	contextSchemaValid(t, m)
	v, err := ValidateContextManifest(encode(t, m), "", opts)
	contextError(t, v, err, "REFERENCE")
}

// F41-01: matching identities and intersecting validity cannot make a
// Manifest usable before it was frozen. This is local declared ordering only;
// actual pre-review delivery still requires the later trusted runtime.
func TestContextUsageCannotPrecedeFreeze(t *testing.T) {
	for _, tc := range []struct {
		name, start string
		reject      bool
	}{
		{"before-freeze", "2026-10-22T01:00:00.000Z", true},
		{"at-freeze", "2026-10-22T03:00:00.000Z", false},
		{"after-freeze", "2026-10-22T03:00:00.001Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := contextFixture(t)
			m.FrozenAt = "2026-10-22T03:00:00.000Z"
			opts := contextCompleteOptions(t, &m)
			opts.RequireCompleteBindings = true
			opts.UsageWindow = &Validity{NotBefore: tc.start, ValidUntil: strptr("2026-10-22T04:00:00.000Z")}
			contextSchemaValid(t, m)
			v, err := ValidateContextManifest(encode(t, m), "", opts)
			if tc.reject {
				contextError(t, v, err, "TIME")
				var e *Error
				errors.As(err, &e)
				if e.Path != "$.usage_window" {
					t.Fatalf("wrong rejection path: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
