package inputartifact

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

func boundedReadSet(value, limit, defaults int) bool {
	if limit <= 0 {
		limit = defaults
	}
	return value <= limit
}
func readSetPoint(v Validity, at time.Time, path string) error {
	iv, e := contextValidity(v, path)
	if e != nil {
		return e
	}
	if at.Before(iv.start) || iv.end != nil && !at.Before(*iv.end) {
		return invalid("TIME", path, "declared event falls outside half-open validity")
	}
	return nil
}
func validateReadSetLocal(b ReadSetSnapshot, o ReadSetValidationOptions) error {
	if !boundedReadSet(len(b.Events), o.MaxEvents, 512) || !boundedReadSet(len(b.DependencyNodes), o.MaxNodes, 2048) {
		return invalid("RESOURCE_LIMIT", "$", "event or graph count ceiling exceeded")
	}
	if b.JournalHighWatermark != int64(len(b.Events)) {
		return invalid("SEQUENCE", "$.journal_high_watermark", "must equal complete finite sequence length")
	}
	frozen, e := parseTime(b.FrozenAt, "$.frozen_at")
	if e != nil {
		return e
	}
	if e = readSetPoint(b.Validity, frozen, "$.validity"); e != nil {
		return e
	}
	if b.FinalSharedInputs.Kind != "SHARED_FACT" {
		return invalid("BINDING", "$.final_shared_inputs.kind", "shared package must be SHARED_FACT")
	}
	if e = fixedVersion(b.SourceDependencyIndexVersion, "$.source_dependency_index_version"); e != nil {
		return e
	}
	if e = ordered(b.DependencyNodes, func(n SourceNode) string { return n.NodeID }, "$.dependency_nodes"); e != nil {
		return e
	}
	nodes := map[string]SourceNode{}
	versions := map[string]SourceNode{}
	for i, n := range b.DependencyNodes {
		p := fmt.Sprintf("$.dependency_nodes[%d]", i)
		if strings.HasPrefix(n.SourceEntryID, "rset_") {
			return invalid("REFERENCE", p, "source may not depend on a future ReadSet identity")
		}
		if e = fixedVersion(n.SourceVersion, p+".source_version"); e != nil {
			return e
		}
		if e = stringsOrdered(n.ParentNodeIDs, p+".parent_node_ids"); e != nil {
			return e
		}
		if n.Kind == "DERIVED" && len(n.ParentNodeIDs) == 0 || n.Kind != "DERIVED" && len(n.ParentNodeIDs) != 0 {
			return invalid("GRAPH", p, "DERIVED needs parents; RAW/EMPTY_RESULT forbid hidden parents")
		}
		if n.RawContentDigest != n.SourceRef.ContentDigest {
			return invalid("IDENTITY", p, "source reference raw digest must equal node raw content digest")
		}
		if _, e = contextValidity(n.Validity, p+".validity"); e != nil {
			return e
		}
		key := n.SourceEntryID + "\x00" + n.SourceVersion
		if prior, ok := versions[key]; ok && !reflect.DeepEqual(prior, n) {
			return invalid("GRAPH", p, "same source/version has incompatible node content")
		}
		versions[key] = n
		nodes[n.NodeID] = n
	}
	// Every dependency must be reachable from an event; neither absent ancestors
	// nor substituted extra nodes are accepted. Bounded DFS checks longest depth.
	visited := map[string]bool{}
	active := map[string]bool{}
	depths := map[string]int{}
	maxDepth := o.MaxGraphDepth
	if maxDepth <= 0 {
		maxDepth = 64
	}
	var visit func(string, int) (int, error)
	visit = func(id string, depth int) (int, error) {
		if depth > maxDepth {
			return 0, invalid("RESOURCE_LIMIT", "$.dependency_nodes", "dependency path exceeds graph depth ceiling")
		}
		if active[id] {
			return 0, invalid("GRAPH", "$.dependency_nodes", "dependency cycle")
		}
		n, ok := nodes[id]
		if !ok {
			return 0, invalid("GRAPH", "$.dependency_nodes", "missing ancestor")
		}
		if visited[id] {
			if depth-1+depths[id] > maxDepth {
				return 0, invalid("RESOURCE_LIMIT", "$.dependency_nodes", "dependency path exceeds graph depth ceiling")
			}
			return depths[id], nil
		}
		active[id] = true
		longest := 1
		for _, p := range n.ParentNodeIDs {
			d, e := visit(p, depth+1)
			if e != nil {
				return 0, e
			}
			longest = max(longest, d+1)
		}
		active[id] = false
		visited[id] = true
		depths[id] = longest
		return longest, nil
	}
	deliveries := map[string]bool{}
	phaseOrder := map[string]int{"INITIAL_REVIEW": 1, "CROSS_EXAMINATION": 2, "FINAL_REVIEW": 3}
	lastPhase := 0
	var lastTime time.Time
	for i, ev := range b.Events {
		p := fmt.Sprintf("$.events[%d]", i)
		if ev.Sequence != int64(i)+1 || ev.JournalKey != b.JournalKey || ev.TargetPersona != b.JournalKey.PersonaID || ev.ExecutionGeneration != b.ExecutionGeneration {
			return invalid("SEQUENCE", p, "consecutive local sequence, exact JournalKey, persona and generation required")
		}
		if i > 0 && ev.AuthoritySequence <= b.Events[i-1].AuthoritySequence {
			return invalid("SEQUENCE", p, "authority order must strictly increase as a declaration")
		}
		if deliveries[ev.DeliveryID] {
			return invalid("DELIVERY", p, "delivery ID reused with a different full event; registry replay cannot append another sequence")
		}
		deliveries[ev.DeliveryID] = true
		if !reflect.DeepEqual(nodes[ev.Source.NodeID], ev.Source) || !slices.Equal(ev.ParentNodeIDs, ev.Source.ParentNodeIDs) {
			return invalid("GRAPH", p, "event source and parent set must equal exact dependency descriptor")
		}
		if _, e = visit(ev.Source.NodeID, 1); e != nil {
			return e
		}
		if phaseOrder[ev.Phase] < lastPhase {
			return invalid("SEQUENCE", p, "phase order regressed")
		}
		lastPhase = phaseOrder[ev.Phase]
		if ev.Kind == "DEBATE" && ev.Phase == "INITIAL_REVIEW" {
			return invalid("PHASE", p, "debate cannot precede cross examination")
		}
		if e = fixedVersion(ev.Producer.IdentitySourceVersion, p+".producer.identity_source_version"); e != nil {
			return e
		}
		at, e := parseTime(ev.InjectedAt, p+".injected_at")
		if e != nil {
			return e
		}
		if at.After(frozen) || i > 0 && at.Before(lastTime) {
			return invalid("TIME", p, "injection time must be ordered and no later than freeze")
		}
		lastTime = at
		if e = readSetPoint(b.Validity, at, p); e != nil {
			return e
		}
		// Ancestors as well as the top-level source must be valid at exposure.
		seen := map[string]bool{}
		var validAncestors func(string) error
		validAncestors = func(id string) error {
			if seen[id] {
				return nil
			}
			seen[id] = true
			n := nodes[id]
			if e := readSetPoint(n.Validity, at, p); e != nil {
				return e
			}
			for _, parent := range n.ParentNodeIDs {
				if e := validAncestors(parent); e != nil {
					return e
				}
			}
			return nil
		}
		if e = validAncestors(ev.Source.NodeID); e != nil {
			return e
		}
		if ev.AuthoritySequence > b.EvidenceCutoffSequence {
			// A cutoff cannot be self-proved by a newly supplied index. Every node in
			// the reread's closure needs a preceding declared event in this Journal.
			known := false
			for j := 0; j < i; j++ {
				if b.Events[j].Source.NodeID == ev.Source.NodeID && b.Events[j].AuthoritySequence <= b.EvidenceCutoffSequence {
					known = true
					break
				}
			}
			if !known {
				return invalid("CUTOFF", p, "post-cutoff event introduces a source version absent from the closed prefix")
			}
		}
	}
	if len(visited) != len(nodes) {
		return invalid("GRAPH", "$.dependency_nodes", "extra node outside exact event ancestor closure")
	}
	return nil
}
