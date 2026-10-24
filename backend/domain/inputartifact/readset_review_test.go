package inputartifact

import (
	"github.com/jamespud/magi/backend/domain/canonical"
	"strings"
	"testing"
)

func TestReadSetReviewCandidateMembershipBypass(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	n := b.DependencyNodes[0]
	n.SourceVersion = "different-version"
	n.RawContentDigest = strings.Repeat("b", 64)
	n.SourceRef.ContentDigest = n.RawContentDigest
	b.DependencyNodes[0] = n
	b.Events[1].Source = n
	binding := o.Sources[n.NodeID]
	binding.Node = n
	binding.ContextSnapshotID = o.ContextManifest.Body().SharedFactSnapshotID
	o.Sources[n.NodeID] = binding
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, &b, &o)
	c, e := canonical.Canonicalize(encode(t, b))
	if e != nil {
		t.Fatal(e)
	}
	if e = readSetSchema(c.Bytes(), "ReadSetSnapshot"); e != nil {
		t.Fatal(e)
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	v, e := ValidateReadSetSnapshot(c.Bytes(), id, o)
	if e == nil {
		t.Fatalf("accepted changed source bytes/version for supplied immutable Candidate ID through shared snapshot membership; status=%+v", v.Status())
	}
}
func TestReadSetReviewClosureBeforeClosedEvent(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	mutateReadSetRecord(t, &b, &o, &b.EvidenceClosureRecordRef, func(m map[string]any) { m["committed_at"] = "2026-10-22T00:00:07.500Z" })
	c, e := canonical.Canonicalize(encode(t, b))
	if e != nil {
		t.Fatal(e)
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	v, e := ValidateReadSetSnapshot(c.Bytes(), id, o)
	if e == nil {
		t.Fatalf("accepted closure at 07.500 before its closed event at 08.000; status=%+v", v.Status())
	}
}
func TestReadSetReviewForeignSnapshotMembershipBypass(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	m := o.ContextManifest.Body()
	var foreign SnapshotRef
	for _, s := range m.SnapshotRefs {
		if s.SnapshotID == m.Roles["casper"].ExperienceSnapshotID {
			foreign = s
		}
	}
	if foreign.SnapshotID == "" {
		t.Fatal("missing fixture")
	}
	n := b.DependencyNodes[0]
	n.SourceEntryID = foreign.SnapshotID
	n.SourceVersion = foreign.Version
	n.RawContentDigest = foreign.ContentDigest
	n.SourceRef = foreign.SourceRef
	n.Validity = foreign.Validity
	b.DependencyNodes[0] = n
	b.Events[1].Source = n
	binding := o.Sources[n.NodeID]
	binding.Node = n
	binding.ContextSnapshotID = m.SharedFactSnapshotID
	o.Sources[n.NodeID] = binding
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, &b, &o)
	c, e := canonical.Canonicalize(encode(t, b))
	if e != nil {
		t.Fatal(e)
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	v, e := ValidateReadSetSnapshot(c.Bytes(), id, o)
	if e == nil {
		t.Fatalf("accepted other-role snapshot under forged shared membership; status=%+v", v.Status())
	}
}

func TestReadSetReviewKnownAncestorReread(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	n := b.DependencyNodes[0]
	original := n
	n.NodeID = "node-derived"
	n.SourceEntryID = "summary-1"
	n.Kind = "DERIVED"
	n.ParentNodeIDs = []string{"node-candidate"}
	n.SourceRef.ObjectID = "summary-1"
	b.DependencyNodes = append(b.DependencyNodes[:1], append([]SourceNode{n}, b.DependencyNodes[1:]...)...)
	b.Events[1].Source = n
	b.Events[1].ParentNodeIDs = n.ParentNodeIDs
	b.Events[1].Kind = "HISTORY_SUMMARY"
	o.Sources[n.NodeID] = ReadSetSourceBinding{Node: n, AdmittedAtSequence: 90}
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, &b, &o)
	readSetValid(t, b, o)
	ev := b.Events[1]
	ev.Sequence = 3
	ev.AuthoritySequence = 101
	ev.DeliveryID = "ancestor-reread"
	ev.Source = original
	ev.ParentNodeIDs = original.ParentNodeIDs
	ev.Kind = "CACHE_REPLAY"
	ev.InjectedAt = "2026-10-22T00:00:10.500Z"
	b.Events = append(b.Events, ev)
	b.JournalHighWatermark = 3
	o.Deliveries[ev.DeliveryID] = ev
	resealReadSetCompleteness(t, &b, &o)
	mutateReadSetRecord(t, &b, &o, &b.CompletenessRecordRef, func(m map[string]any) { m["recorded_at"] = "2026-10-22T00:00:10.500Z" })
	if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e != nil {
		t.Fatalf("rejected exact pre-cutoff admitted ancestor reread: %v", e)
	}
}

func TestReadSetReviewEventUsesClosureAuthoritySequence(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	b.Events[1].AuthoritySequence = b.EvidenceCutoffSequence
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, &b, &o)
	c, e := canonical.Canonicalize(encode(t, b))
	if e != nil {
		t.Fatal(e)
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	v, e := ValidateReadSetSnapshot(c.Bytes(), id, o)
	if e == nil {
		t.Fatalf("accepted read event using the EVIDENCE_CLOSED authority sequence; status=%+v", v.Status())
	}
}

// Keep every event/index/completeness descriptor consistent so these tests reach
// immutable source identity binding instead of an outer declaration mismatch.
func readSetReviewSource(t *testing.T, b *ReadSetSnapshot, o *ReadSetValidationOptions, n SourceNode, scope string, ancestor bool) {
	t.Helper()
	b.DependencyNodes[0] = n
	binding := o.Sources[n.NodeID]
	binding.Node = n
	binding.ContextSnapshotID = scope
	o.Sources[n.NodeID] = binding
	if ancestor {
		derived := n
		derived.NodeID = "node-derived"
		derived.SourceEntryID = "summary-review"
		derived.Kind = "DERIVED"
		derived.SourceRef.ObjectID = "summary-review"
		derived.ParentNodeIDs = []string{n.NodeID}
		b.DependencyNodes = append(b.DependencyNodes[:1], append([]SourceNode{derived}, b.DependencyNodes[1:]...)...)
		o.Sources[derived.NodeID] = ReadSetSourceBinding{Node: derived, AdmittedAtSequence: 90}
		b.Events[1].Source = derived
		b.Events[1].ParentNodeIDs = derived.ParentNodeIDs
		b.Events[1].Kind = "HISTORY_SUMMARY"
	} else {
		b.Events[1].Source = n
		b.Events[1].ParentNodeIDs = n.ParentNodeIDs
	}
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, b, o)
}
func readSetReviewValidate(t *testing.T, b ReadSetSnapshot, o ReadSetValidationOptions) (ValidatedReadSetSnapshot, error) {
	t.Helper()
	c, e := canonical.CanonicalizeWithLimits(encode(t, b), readSetLimits(ResourceLimits{}))
	if e != nil {
		t.Fatal(e)
	}
	if e = readSetSchema(c.Bytes(), "ReadSetSnapshot"); e != nil {
		t.Fatalf("not a schema-valid semantic mutation: %v", e)
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	return ValidateReadSetSnapshot(c.Bytes(), id, o)
}
func TestReadSetReviewKnownIdentityIgnoresFakeLabels(t *testing.T) {
	for _, identity := range []string{"candidate", "foreign_snapshot", "own_snapshot"} {
		for _, layout := range []string{"direct", "ancestor", "derived_identity"} {
			t.Run(identity+"/"+layout, func(t *testing.T) {
				b, o := readSetCompleteOptions(t)
				o.RequireCompleteBindings = true
				readSetValid(t, b, o)
				m := o.ContextManifest.Body()
				n := b.DependencyNodes[0]
				if identity == "candidate" {
					n.SourceVersion = "different-version"
					n.RawContentDigest = strings.Repeat("b", 64)
					n.SourceRef.ContentDigest = n.RawContentDigest
				} else {
					id := m.Roles["casper"].ExperienceSnapshotID
					if identity == "own_snapshot" {
						id = m.Roles["melchior"].ExperienceSnapshotID
					}
					for _, s := range m.SnapshotRefs {
						if s.SnapshotID == id {
							n.SourceEntryID = s.SnapshotID
							n.SourceVersion = s.Version
							n.RawContentDigest = s.ContentDigest
							n.SourceRef = s.SourceRef
							n.Validity = s.Validity
						}
					}
					if identity == "own_snapshot" {
						n.RawContentDigest = strings.Repeat("b", 64)
						n.SourceRef.ContentDigest = n.RawContentDigest
					}
				}
				if layout == "derived_identity" {
					n.Kind = "DERIVED"
					n.ParentNodeIDs = []string{"node-shared"}
				}
				readSetReviewSource(t, &b, &o, n, m.SharedFactSnapshotID, layout == "ancestor")
				_, e := readSetReviewValidate(t, b, o)
				detail, ok := e.(*Error)
				if !ok || detail.Kind != "BINDING" || detail.Path != "$.sources" {
					t.Fatalf("expected inner known-source binding rejection, got %v", e)
				}
			})
		}
	}
}
func TestReadSetReviewKnownIdentityMembership(t *testing.T) {
	for _, identity := range []string{"candidate", "own_snapshot"} {
		t.Run(identity, func(t *testing.T) {
			b, o := readSetCompleteOptions(t)
			o.RequireCompleteBindings = true
			m := o.ContextManifest.Body()
			n := b.DependencyNodes[0]
			scope := b.CandidateSnapshotID
			if identity == "own_snapshot" {
				scope = m.Roles["melchior"].ExperienceSnapshotID
				for _, s := range m.SnapshotRefs {
					if s.SnapshotID == scope {
						n.SourceEntryID = s.SnapshotID
						n.SourceVersion = s.Version
						n.RawContentDigest = s.ContentDigest
						n.SourceRef = s.SourceRef
						n.Validity = s.Validity
					}
				}
			}
			readSetReviewSource(t, &b, &o, n, scope, false)
			if _, e := readSetReviewValidate(t, b, o); e != nil {
				t.Fatalf("proper exact source binding rejected: %v", e)
			}
			binding := o.Sources[n.NodeID]
			binding.ContextSnapshotID = m.SharedFactSnapshotID
			o.Sources[n.NodeID] = binding
			if _, e := readSetReviewValidate(t, b, o); e == nil {
				t.Fatal("known immutable identity accepted contradictory membership label")
			}
		})
	}
}
func TestReadSetReviewClosureTimeline(t *testing.T) {
	for _, name := range []string{"prefix_after_close", "reread_before_close", "equal_prefix_time", "equal_reread_time"} {
		t.Run(name, func(t *testing.T) {
			b, o := readSetCompleteOptions(t)
			o.RequireCompleteBindings = true
			readSetValid(t, b, o)
			if name == "prefix_after_close" || name == "equal_prefix_time" {
				stamp := "2026-10-22T00:00:07.500Z"
				if name == "equal_prefix_time" {
					stamp = "2026-10-22T00:00:08.000Z"
				}
				mutateReadSetRecord(t, &b, &o, &b.EvidenceClosureRecordRef, func(m map[string]any) { m["committed_at"] = stamp })
			} else {
				ev := b.Events[0]
				ev.Sequence = 3
				ev.AuthoritySequence = 101
				ev.DeliveryID = "review-reread"
				ev.Kind = "CACHE_REPLAY"
				ev.InjectedAt = "2026-10-22T00:00:10.500Z"
				b.Events = append(b.Events, ev)
				b.JournalHighWatermark = 3
				o.Deliveries[ev.DeliveryID] = ev
				resealReadSetCompleteness(t, &b, &o)
				mutateReadSetRecord(t, &b, &o, &b.CompletenessRecordRef, func(m map[string]any) { m["recorded_at"] = "2026-10-22T00:00:10.500Z" })
				readSetValid(t, b, o)
				stamp := "2026-10-22T00:00:09.000Z"
				if name == "equal_reread_time" {
					stamp = "2026-10-22T00:00:10.000Z"
				}
				b.Events[2].InjectedAt = stamp
				o.Deliveries[ev.DeliveryID] = b.Events[2]
				resealReadSetCompleteness(t, &b, &o)
			}
			_, e := readSetReviewValidate(t, b, o)
			wantError := name == "prefix_after_close" || name == "reread_before_close"
			if (e != nil) != wantError {
				t.Fatalf("closure timeline contradiction=%v, error=%v", wantError, e)
			}
			if wantError {
				detail, ok := e.(*Error)
				if !ok || detail.Kind != "BINDING" || detail.Path != "$.closure.committed_at" {
					t.Fatalf("did not reach inner closure time check: %v", e)
				}
			}
		})
	}
}
func TestReadSetReviewAdmissionReservesClosureSequence(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	readSetValid(t, b, o)
	binding := o.Sources["node-candidate"]
	binding.AdmittedAtSequence = b.EvidenceCutoffSequence
	o.Sources["node-candidate"] = binding
	if _, e := readSetReviewValidate(t, b, o); e == nil {
		t.Fatal("admission reused closure authority slot")
	}
}
func TestReadSetReviewAncestorRereadNewVersionRejected(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	original := b.DependencyNodes[0]
	readSetReviewSource(t, &b, &o, original, b.CandidateSnapshotID, true)
	readSetValid(t, b, o)
	changed := original
	changed.NodeID = "node-replay-2"
	changed.SourceVersion = "2"
	changed.RawContentDigest = strings.Repeat("b", 64)
	changed.SourceRef.ContentDigest = changed.RawContentDigest
	b.DependencyNodes = append(b.DependencyNodes[:2], append([]SourceNode{changed}, b.DependencyNodes[2:]...)...)
	o.Sources[changed.NodeID] = ReadSetSourceBinding{Node: changed, ContextSnapshotID: b.CandidateSnapshotID, AdmittedAtSequence: 90}
	ev := b.Events[1]
	ev.Source = changed
	ev.ParentNodeIDs = changed.ParentNodeIDs
	ev.Sequence = 3
	ev.AuthoritySequence = 101
	ev.DeliveryID = "new-ancestor-version"
	ev.InjectedAt = "2026-10-22T00:00:10.500Z"
	ev.Kind = "CACHE_REPLAY"
	b.Events = append(b.Events, ev)
	b.JournalHighWatermark = 3
	o.Deliveries[ev.DeliveryID] = ev
	resealReadSetCompleteness(t, &b, &o)
	mutateReadSetRecord(t, &b, &o, &b.CompletenessRecordRef, func(m map[string]any) { m["recorded_at"] = "2026-10-22T00:00:10.500Z" })
	_, e := readSetReviewValidate(t, b, o)
	detail, ok := e.(*Error)
	if !ok || detail.Kind != "CUTOFF" {
		t.Fatalf("new version did not fail at closed-prefix admission: %v", e)
	}
}
