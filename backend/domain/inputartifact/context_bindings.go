package inputartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

var contextCharterID = regexp.MustCompile(`^chtr_[0-9a-f]{64}$`)
var contextReleaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// checkContextBindings checks caller declarations against immutable local
// artifacts. It deliberately has no success-capable authority resolver.
func checkContextBindings(m ContextManifest, opts ContextValidationOptions, iv contextInterval) ([]Dependency, ContextValidationStatus, error) {
	status := ContextValidationStatus{Structure: "VALID_OFFLINE", Bindings: "CHECKED_OFFLINE", SnapshotBytes: "CHECKED_OFFLINE", Qualification: "UNVERIFIABLE_REFERENCE", ActualReads: "NOT_RUN"}
	var pending []Dependency
	missing := false
	unresolved := func(path, detail string) {
		pending = append(pending, Dependency{Kind: "UNVERIFIABLE_REFERENCE", Path: path, Detail: detail})
	}
	unavailable := func(path, detail string) { missing = true; unresolved(path, detail) }
	bindErr := func(path, detail string) ([]Dependency, ContextValidationStatus, error) {
		return nil, ContextValidationStatus{}, invalid("BINDING", path, detail)
	}
	mergeValidity := func(v Validity, path string) error {
		w, e := contextValidity(v, path)
		if e != nil {
			return e
		}
		iv, e = intersectContext(iv, w, path)
		return e
	}
	propagate := func(deps []Dependency, prefix string) {
		for _, d := range deps {
			d.Path = prefix + strings.TrimPrefix(d.Path, "$")
			pending = append(pending, d)
		}
	}
	if opts.Selection == nil {
		status.Bindings = "UNVERIFIABLE_REFERENCE"
		unavailable("$.selection", "no external exact case/round/role/Charter/scope selection supplied; body cannot establish its own role ownership")
	} else {
		s := opts.Selection
		c := s.Charter
		// All declarations are checked even if domain/persona/bytes are unavailable.
		if c.SchemaVersion != "2.0" {
			return nil, ContextValidationStatus{}, invalid("VERSION", "$.selection.charter.schema_version", "only explicitly selected Charter v2 declarations are supported")
		}
		if e := fixedVersion(c.Version, "$.selection.charter.version"); e != nil {
			return nil, ContextValidationStatus{}, e
		}
		if !contextReleaseVersion.MatchString(c.Version) {
			return nil, ContextValidationStatus{}, invalid("VERSION", "$.selection.charter.version", "exact declared release version required")
		}
		if !contextCharterID.MatchString(c.ArtifactID) || c.ArtifactID != m.CharterID || c.DomainProfileID != m.DomainProfileID {
			return bindErr("$.charter_id", "selected Charter exact identity and declared domain must match Manifest")
		}
		if s.CaseID != m.CaseID || s.Round < 1 || s.Round != m.Round {
			return bindErr("$.case_id", "case and positive decision round must equal external selection")
		}
		if s.ContextRevocationScopeID != m.ContextRevocationScopeID || s.ContextRevocationEpoch < 0 || s.ContextRevocationEpoch != m.ContextRevocationEpoch {
			return bindErr("$.context_revocation_scope_id", "scope and epoch must match together; no bare epoch comparison")
		}
		if s.SharedFactSnapshotID != m.SharedFactSnapshotID || s.UserPreferenceSnapshotID != m.UserPreferenceSnapshotID || s.SourceDependencyIndexVersion != m.SourceDependencyIndexVersion {
			return bindErr("$.source_dependency_index_version", "initial fact/preference/index selection mismatch")
		}
		if s.AdmissionPolicyRef != m.AdmissionPolicyRef {
			return bindErr("$.admission_policy_ref", "admission policy must match exact selected reference")
		}
		if len(s.Roles) != len(manifestRoles) {
			return bindErr("$.roles", "selection requires exactly the three formal roles")
		}
		for _, role := range manifestRoles {
			want, ok := s.Roles[role]
			got := m.Roles[role]
			if !ok || want.PersonaSpecID != got.PersonaSpecID || want.ExperienceSnapshotID != got.ExperienceSnapshotID || !slices.Equal(want.AllowedContextIDs, got.AllowedContextIDs) {
				return bindErr("$.roles."+role, "Persona/experience/ordered allowed-context selection mismatch")
			}
		}
		if e := mergeValidity(c.Validity, "$.selection.charter.validity"); e != nil {
			return nil, ContextValidationStatus{}, e
		}
		horizon, e := parseTime(c.ValidityHorizon, "$.selection.charter.validity_horizon")
		if e != nil {
			return nil, ContextValidationStatus{}, e
		}
		ci, e := contextValidity(c.Validity, "$.selection.charter.validity")
		if e != nil {
			return nil, ContextValidationStatus{}, e
		}
		if !ci.start.Before(horizon) {
			return nil, ContextValidationStatus{}, invalid("TIME", "$.selection.charter.validity_horizon", "Charter horizon must follow validity start")
		}
		ci.end = &horizon
		iv, e = intersectContext(iv, ci, "$.selection.charter.validity_horizon")
		if e != nil {
			return nil, ContextValidationStatus{}, e
		}
	}
	unresolved("$.charter_id", "Charter binding is an unauthenticated external declaration; original Charter bytes/Schema/approval/disclosure constraints and v2 consumer compatibility are not verified")
	if opts.DomainProfile == nil {
		status.Bindings = "PARTIAL_OFFLINE"
		unavailable("$.domain_profile_id", "selected validated DomainProfile bytes unavailable")
	} else {
		d := *opts.DomainProfile
		if d.ArtifactID() == "" || d.ArtifactID() != m.DomainProfileID {
			return bindErr("$.domain_profile_id", "nonzero validated domain must match exact Manifest identity")
		}
		if opts.Selection != nil && opts.Selection.DomainProfileVersion != d.Body().Version {
			return bindErr("$.domain_profile_id", "selected domain release version differs from validated bytes")
		}
		if e := mergeValidity(d.Body().Validity, "$.domain_profile.validity"); e != nil {
			return nil, ContextValidationStatus{}, e
		}
		propagate(d.PendingDependencies(), "$.domain_profile")
	}
	for role := range opts.Personas {
		if !slices.Contains(manifestRoles[:], role) {
			return bindErr("$.roles", "unknown supplied Persona role")
		}
	}
	for _, role := range manifestRoles {
		path := "$.roles." + role + ".persona_spec_id"
		p, ok := opts.Personas[role]
		if !ok {
			status.Bindings = "PARTIAL_OFFLINE"
			unavailable(path, "selected valid offline PersonaSpec bytes unavailable")
			continue
		}
		if p.ArtifactID() == "" || p.ArtifactID() != m.Roles[role].PersonaSpecID || p.Body().PersonaID != role {
			return bindErr(path, "nonzero validated Persona exact ID and declared role must match selected view")
		}
		if p.DomainProfileID() != "" && p.DomainProfileID() != m.DomainProfileID {
			return bindErr(path, "Persona was validated against a different domain identity")
		}
		if p.DomainProfileID() == "" || p.Status().DomainRelations == "UNVERIFIABLE_REFERENCE" {
			status.Bindings = "PARTIAL_OFFLINE"
			unavailable(path, "Persona has no checked selected domain context")
		}
		if e := mergeValidity(p.Body().Validity, "$.roles."+role+".persona.validity"); e != nil {
			return nil, ContextValidationStatus{}, e
		}
		propagate(p.PendingDependencies(), "$.roles."+role+".persona")
	}
	refs := map[string]bool{}
	for _, s := range m.SnapshotRefs {
		refs[s.SnapshotID] = true
	}
	for id := range opts.Snapshots {
		if !refs[id] {
			return bindErr("$.snapshot_refs", "supplied snapshot material has no Manifest descriptor")
		}
	}
	per, total := opts.MaxSnapshotBytes, opts.MaxTotalSnapshotBytes
	if per <= 0 {
		per = 262144
	}
	if total <= 0 {
		total = 1048576
	}
	remaining := total
	for i, s := range m.SnapshotRefs {
		path := fmt.Sprintf("$.snapshot_refs[%d]", i)
		blob, ok := opts.Snapshots[s.SnapshotID]
		if !ok {
			status.SnapshotBytes = "PARTIAL_OFFLINE"
			unavailable(path, "external exact descriptor and original bytes unavailable")
		} else {
			if !reflect.DeepEqual(blob.Descriptor, s) {
				return bindErr(path, "external descriptor must match ID/kind/version/raw digest/references/validity exactly")
			}
			if blob.RawBytes == nil {
				status.SnapshotBytes = "PARTIAL_OFFLINE"
				unavailable(path, "original raw snapshot bytes unavailable")
			} else {
				if len(blob.RawBytes) > per || len(blob.RawBytes) > remaining {
					return nil, ContextValidationStatus{}, invalid("RESOURCE_LIMIT", path, "raw snapshot per-blob or aggregate byte ceiling exceeded")
				}
				remaining -= len(blob.RawBytes)
				digest := sha256.Sum256(blob.RawBytes)
				if hex.EncodeToString(digest[:]) != s.ContentDigest {
					return nil, ContextValidationStatus{}, invalid("IDENTITY", path+".content_digest", "raw-byte SHA-256 mismatch; no C0 prefix or normalization allowed")
				}
			}
		}
		unresolved(path+".qualification", "descriptor/raw digest checks do not authenticate source kind, role ownership, access/disclosure scope, complete contents, empty-snapshot semantics, freshness or publication")
	}
	if opts.UsageWindow != nil {
		usage, e := contextValidity(*opts.UsageWindow, "$.usage_window")
		if e != nil {
			return nil, ContextValidationStatus{}, e
		}
		if e = coversContext(iv, usage, "$.usage_window"); e != nil {
			return nil, ContextValidationStatus{}, e
		}
	} else {
		unresolved("$.validity", "no usage window supplied; current validity and future freshness not checked")
	}
	frozen, e := parseTime(m.FrozenAt, "$.frozen_at")
	if e != nil {
		return nil, ContextValidationStatus{}, e
	}
	if iv.end != nil && !frozen.Before(*iv.end) {
		return nil, ContextValidationStatus{}, invalid("TIME", "$.frozen_at", "a known dependency expired before Manifest freeze")
	}
	unresolved("$.user_preference_snapshot_id", "authentic user preference, empty-preference confirmation policy and consent are not verified")
	unresolved("$.admission_policy_ref", "admission/disclosure policy compatibility and external access qualification are unverified; no runtime admission performed")
	unresolved("$.context_revocation_scope_id", "scope registration and current authoritative epoch/revocation ordering require C2/C3; numeric equality is local only")
	unresolved("$.case_id", "unique (case,round)->Manifest registration and pre-review timing require external authoritative state")
	if missing && opts.RequireCompleteBindings {
		return nil, ContextValidationStatus{}, invalid("UNVERIFIABLE_REFERENCE", "$", "complete local binding evidence unavailable")
	}
	return pending, status, nil
}
