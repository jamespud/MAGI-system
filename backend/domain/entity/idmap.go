package entity

import (
	"sort"
	"strings"
)

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
//
// It is a single left-to-right pass, which makes it idempotent and safe:
//
//   - Already-persisted (namespaced) values are matched first and copied
//     verbatim, so a namespaced ID that contains its bare form as a suffix is
//     never rewritten into a doubled prefix.
//   - Bare IDs are replaced longest-match-first and only on token boundaries,
//     so "EV-001" can neither partially rewrite "EV-0012" nor rewrite the
//     "EV-001" inside an unrelated "...-EV-001" identifier.
//   - Matching does not use sentinel/placeholder tokens, so control characters
//     or other text in the report can never collide with the rewriting.
func (m *ArtifactRemap) RemapText(s string) string {
	if m == nil || s == "" {
		return s
	}
	namespaced := make(map[byte][]string, len(m.evidence)+len(m.claims))
	bare := make(map[byte][]remapPair, len(m.evidence)+len(m.claims))
	add := func(old, new string) {
		if old == "" || new == "" || old == new {
			return
		}
		namespaced[new[0]] = append(namespaced[new[0]], new)
		bare[old[0]] = append(bare[old[0]], remapPair{old: old, new: new})
	}
	for old, new := range m.evidence {
		add(old, new)
	}
	for old, new := range m.claims {
		add(old, new)
	}
	for b := range namespaced {
		sort.Slice(namespaced[b], func(i, j int) bool { return len(namespaced[b][i]) > len(namespaced[b][j]) })
	}
	for b := range bare {
		sort.Slice(bare[b], func(i, j int) bool { return len(bare[b][i].old) > len(bare[b][j].old) })
	}

	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if new, ok := matchNamespaced(s, i, namespaced[s[i]]); ok {
			out = append(out, new...)
			i += len(new)
			continue
		}
		if pair, ok := matchBare(s, i, bare[s[i]]); ok {
			out = append(out, pair.new...)
			i += len(pair.old)
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

type remapPair struct{ old, new string }

// matchNamespaced returns the longest already-persisted id that starts at i on
// a token boundary.
func matchNamespaced(s string, i int, candidates []string) (string, bool) {
	for _, cand := range candidates {
		if strings.HasPrefix(s[i:], cand) && idTokenBoundary(s, i, len(cand)) {
			return cand, true
		}
	}
	return "", false
}

// matchBare returns the longest bare id that starts at i on a token boundary.
func matchBare(s string, i int, candidates []remapPair) (remapPair, bool) {
	for _, cand := range candidates {
		if strings.HasPrefix(s[i:], cand.old) && idTokenBoundary(s, i, len(cand.old)) {
			return cand, true
		}
	}
	return remapPair{}, false
}

// idTokenBoundary reports whether the substring s[i:i+n] is delimited by
// characters that cannot be part of an artifact id.
func idTokenBoundary(s string, i, n int) bool {
	if i > 0 && isIDByte(s[i-1]) {
		return false
	}
	if j := i + n; j < len(s) && isIDByte(s[j]) {
		return false
	}
	return true
}

func isIDByte(b byte) bool {
	switch {
	case b >= '0' && b <= '9', b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z':
		return true
	case b == '-' || b == '_':
		return true
	default:
		return false
	}
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
// deterministic: "<case>-<code>[-g<generation>|-a<attempt>]-r<round>-<phase>".
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
			if codeIdx-1 >= 0 && isExecutionMarker(parts[codeIdx]) {
				codeIdx--
			}
			if codeIdx >= 0 {
				return MagiCode(parts[codeIdx])
			}
		}
	}
	return ""
}

func isExecutionMarker(s string) bool {
	if len(s) < 2 || (s[0] != 'a' && s[0] != 'g') {
		return false
	}
	return isDigits(s[1:])
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
