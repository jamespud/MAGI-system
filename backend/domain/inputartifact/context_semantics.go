package inputartifact

import (
	"fmt"
	"time"
)

var manifestRoles = [...]string{"melchior", "balthasar", "casper"}

// An interval is half-open [start,end); a nil end is an unknown/open upper
// bound. Declared intersection never establishes current external validity.
type contextInterval struct {
	start time.Time
	end   *time.Time
}

func contextValidity(v Validity, path string) (contextInterval, error) {
	start, e := parseTime(v.NotBefore, path+".not_before")
	if e != nil {
		return contextInterval{}, e
	}
	iv := contextInterval{start: start}
	if v.ValidUntil != nil {
		end, e := parseTime(*v.ValidUntil, path+".valid_until")
		if e != nil {
			return contextInterval{}, e
		}
		if !start.Before(end) {
			return contextInterval{}, invalid("TIME", path, "not_before must precede valid_until")
		}
		iv.end = &end
	}
	return iv, nil
}
func intersectContext(a, b contextInterval, path string) (contextInterval, error) {
	if b.start.After(a.start) {
		a.start = b.start
	}
	if b.end != nil && (a.end == nil || b.end.Before(*a.end)) {
		a.end = b.end
	}
	if a.end != nil && !a.start.Before(*a.end) {
		return contextInterval{}, invalid("TIME", path, "known dependency validity intervals have no common usage interval")
	}
	return a, nil
}
func coversContext(outer, inner contextInterval, path string) error {
	if inner.start.Before(outer.start) || (outer.end != nil && (inner.end == nil || inner.end.After(*outer.end))) {
		return invalid("TIME", path, "declared usage window exceeds a known dependency validity or Charter horizon")
	}
	return nil
}

func validateContextLocal(m ContextManifest) (contextInterval, error) {
	frozen, e := parseTime(m.FrozenAt, "$.frozen_at")
	if e != nil {
		return contextInterval{}, e
	}
	iv, e := contextValidity(m.Validity, "$.validity")
	if e != nil {
		return contextInterval{}, e
	}
	if iv.end != nil && !frozen.Before(*iv.end) {
		return contextInterval{}, invalid("TIME", "$.frozen_at", "Manifest frozen after its declared validity expired")
	}
	if e = fixedVersion(m.Version, "$.version"); e != nil {
		return contextInterval{}, e
	}
	if e = ordered(m.SnapshotRefs, func(s SnapshotRef) string { return s.SnapshotID }, "$.snapshot_refs"); e != nil {
		return contextInterval{}, e
	}
	required := map[string]string{}
	add := func(id, kind, path string) error {
		if e := fixedVersion(id, path); e != nil {
			return e
		}
		if prior, ok := required[id]; ok && prior != kind {
			return invalid("REFERENCE", path, "snapshot ID reused across different kinds")
		}
		required[id] = kind
		return nil
	}
	for _, r := range []struct{ id, kind, path string }{
		{m.SharedFactSnapshotID, "SHARED_FACT", "$.shared_fact_snapshot_id"},
		{m.UserPreferenceSnapshotID, "USER_PREFERENCE", "$.user_preference_snapshot_id"},
		{m.SourceDependencyIndexVersion, "DEPENDENCY_INDEX", "$.source_dependency_index_version"},
	} {
		if e = add(r.id, r.kind, r.path); e != nil {
			return contextInterval{}, e
		}
	}
	experiences := map[string]bool{}
	personas := map[string]bool{}
	for _, role := range manifestRoles {
		view := m.Roles[role]
		path := "$.roles." + role
		if personas[view.PersonaSpecID] {
			return contextInterval{}, invalid("REFERENCE", path+".persona_spec_id", "distinct formal roles cannot share a PersonaSpec identity")
		}
		personas[view.PersonaSpecID] = true
		if experiences[view.ExperienceSnapshotID] {
			return contextInterval{}, invalid("REFERENCE", path+".experience_snapshot_id", "role experience must be independently fixed; implicit inheritance is forbidden")
		}
		experiences[view.ExperienceSnapshotID] = true
		if e = add(view.ExperienceSnapshotID, "EXPERIENCE", path+".experience_snapshot_id"); e != nil {
			return contextInterval{}, e
		}
		if e = stringsOrdered(view.AllowedContextIDs, path+".allowed_context_ids"); e != nil {
			return contextInterval{}, e
		}
		for i, id := range view.AllowedContextIDs {
			if e = add(id, "CONTEXT", fmt.Sprintf("%s.allowed_context_ids[%d]", path, i)); e != nil {
				return contextInterval{}, e
			}
		}
	}
	for i, s := range m.SnapshotRefs {
		path := fmt.Sprintf("$.snapshot_refs[%d]", i)
		if e = fixedVersion(s.SnapshotID, path+".snapshot_id"); e != nil {
			return contextInterval{}, e
		}
		if e = fixedVersion(s.Version, path+".version"); e != nil {
			return contextInterval{}, e
		}
		kind, ok := required[s.SnapshotID]
		if !ok || kind != s.Kind {
			return contextInterval{}, invalid("REFERENCE", path, "descriptor must have the exact required snapshot ID and kind; no orphan descriptors")
		}
		delete(required, s.SnapshotID)
		sv, e := contextValidity(s.Validity, path+".validity")
		if e != nil {
			return contextInterval{}, e
		}
		iv, e = intersectContext(iv, sv, path+".validity")
		if e != nil {
			return contextInterval{}, e
		}
	}
	if len(required) != 0 {
		return contextInterval{}, invalid("REFERENCE", "$.snapshot_refs", "descriptor closure is missing required IDs")
	}
	if iv.end != nil && !frozen.Before(*iv.end) {
		return contextInterval{}, invalid("TIME", "$.frozen_at", "initial dependencies have expired before freezing")
	}
	return iv, nil
}
