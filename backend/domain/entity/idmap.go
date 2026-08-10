package entity

import "strings"

// ArtifactRemap maps in-memory ledger IDs (EV-001/CL-001) to the namespaced
// IDs persisted for one dispatch round ("<runID>-<oldID>"). It is the single
// source of truth for ID rewriting at runtime and is reused by the historical
// data migration so both paths stay consistent. It lives in entity so both
// the orchestrator and the memory projection can use it without an import
// cycle (orchestration -> memory -> orchestration).
type ArtifactRemap struct {
	evidence map[string]string // old EV-ID -> persisted EV-ID
	claims   map[string]string // old CL-ID -> persisted CL-ID
}

// NewArtifactRemap creates an empty remap.
func NewArtifactRemap() *ArtifactRemap {
	return &ArtifactRemap{
		evidence: make(map[string]string),
		claims:   make(map[string]string),
	}
}

// AddEvidence registers an EV-ID mapping only.
func (m *ArtifactRemap) AddEvidence(old, new string) {
	if m != nil && old != "" && new != "" {
		m.evidence[old] = new
	}
}

// AddClaim registers a CL-ID mapping only.
func (m *ArtifactRemap) AddClaim(old, new string) {
	if m != nil && old != "" && new != "" {
		m.claims[old] = new
	}
}

// EvidenceMap exposes the EV-ID mapping as a plain map for raw-map rewrites.
func (m *ArtifactRemap) EvidenceMap() map[string]string {
	if m == nil {
		return nil
	}
	return m.evidence
}

// ClaimMap exposes the CL-ID mapping as a plain map for raw-map rewrites.
func (m *ArtifactRemap) ClaimMap() map[string]string {
	if m == nil {
		return nil
	}
	return m.claims
}

// RemapList rewrites every element of ids that appears in the remap.
func (m *ArtifactRemap) RemapList(ids []string) []string {
	if m == nil || len(ids) == 0 {
		return ids
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = m.lookup(id)
	}
	return out
}

// RemapText replaces every known in-memory ID inside a text blob with its
// persisted form. Used for the final report body, whose citations are
// free-form text rather than structured fields.
func (m *ArtifactRemap) RemapText(s string) string {
	if m == nil || s == "" {
		return s
	}
	for old, new := range m.evidence {
		if old != "" {
			s = strings.ReplaceAll(s, old, new)
		}
	}
	for old, new := range m.claims {
		if old != "" {
			s = strings.ReplaceAll(s, old, new)
		}
	}
	return s
}

// Merge folds other's mappings into m.
func (m *ArtifactRemap) Merge(other *ArtifactRemap) {
	if m == nil || other == nil {
		return
	}
	for k, v := range other.evidence {
		m.evidence[k] = v
	}
	for k, v := range other.claims {
		m.claims[k] = v
	}
}

func (m *ArtifactRemap) lookup(id string) string {
	if m == nil {
		return id
	}
	if v, ok := m.evidence[id]; ok {
		return v
	}
	if v, ok := m.claims[id]; ok {
		return v
	}
	return id
}

// CodeOfRun extracts the MagiCode from a persisted agent run ID. Run IDs are
// deterministic: "<case>-<code>[-a<attempt>]-r<round>-<phase>".
func CodeOfRun(runID string) MagiCode {
	if runID == "" {
		return ""
	}
	parts := strings.Split(runID, "-")
	// Case IDs themselves contain dashes ("case-<uuid>"), so scan from the
	// right: find "-r<round>-<phase>"; the code is the part two before it when
	// an attempt marker is present, one before it otherwise.
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if strings.HasPrefix(p, "r") && len(p) > 1 && isDigits(p[1:]) {
			// Skip the trailing phase segment if present.
			codeIdx := i - 1
			if codeIdx-1 >= 0 && strings.HasPrefix(parts[codeIdx], "a") && isDigits(parts[codeIdx][1:]) {
				codeIdx--
			}
			if codeIdx >= 0 {
				return MagiCode(parts[codeIdx])
			}
		}
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
