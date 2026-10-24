package inputartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jamespud/magi/backend/domain/canonical"
	"os"
	"reflect"
	"strings"
	"testing"
)

type readSetFixture struct {
	Body      ReadSetSnapshot `json:"body"`
	Candidate struct {
		ArtifactID string          `json:"artifact_id"`
		Body       json.RawMessage `json:"body"`
	} `json:"candidate"`
	Records map[string]struct {
		Ref  ExternalRef     `json:"ref"`
		Body json.RawMessage `json:"body"`
	} `json:"records"`
}

func readSetOptions(t *testing.T) (ReadSetSnapshot, ReadSetValidationOptions) {
	t.Helper()
	var f readSetFixture
	if e := json.Unmarshal(read(t, "testdata/readset_v2_fixture.json"), &f); e != nil {
		t.Fatal(e)
	}
	m := contextFixture(t)
	ctx, e := ValidateContextManifest(encode(t, m), "", contextOptions(t, m))
	if e != nil {
		t.Fatal(e)
	}
	return readSetOptionsFor(t, f, ctx)
}
func readSetOptionsFor(t *testing.T, f readSetFixture, ctx ValidatedContextManifest) (ReadSetSnapshot, ReadSetValidationOptions) {
	t.Helper()
	m := ctx.Body()
	b := f.Body
	o := ReadSetValidationOptions{ContextManifest: &ctx, SourceDependencyIndexVersion: b.SourceDependencyIndexVersion, Selection: &ReadSetSelection{JournalKey: b.JournalKey, ExecutionGeneration: b.ExecutionGeneration, ContextRevocationScopeID: b.ContextRevocationScopeID, ContextRevocationEpoch: b.ContextRevocationEpoch, AuthorityScopeID: b.AuthorityScopeID, AuthorityEpoch: b.AuthorityEpoch, CredentialScopeID: b.CredentialScopeID, CredentialEpoch: b.CredentialEpoch}, Candidate: &ReadSetCandidate{ArtifactID: f.Candidate.ArtifactID, RawBytes: canonicalTestBytes(t, f.Candidate.Body)}, Sources: map[string]ReadSetSourceBinding{}, Deliveries: map[string]ReadEvent{}, Materials: map[string]ReadSetMaterial{}}
	for _, n := range b.DependencyNodes {
		scope := m.SharedFactSnapshotID
		if n.NodeID == "node-candidate" {
			scope = b.CandidateSnapshotID
		}
		o.Sources[n.NodeID] = ReadSetSourceBinding{Node: n, ContextSnapshotID: scope, AdmittedAtSequence: 90}
	}
	for _, ev := range b.Events {
		o.Deliveries[ev.DeliveryID] = ev
	}
	for _, r := range f.Records {
		o.Materials[refKey(r.Ref)] = ReadSetMaterial{Reference: r.Ref, RawBytes: canonicalTestBytes(t, r.Body)}
	}
	return b, o
}
func canonicalTestBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	c, e := canonical.CanonicalizeWithLimits(b, canonical.Limits{MaxBytes: 1048576, MaxDepth: 32, MaxMembers: 65536, MaxStringBytes: 16384})
	if e != nil {
		t.Fatal(e)
	}
	return c.Bytes()
}
func readSetValid(t *testing.T, b ReadSetSnapshot, o ReadSetValidationOptions) ValidatedReadSetSnapshot {
	t.Helper()
	v, e := ValidateReadSetSnapshot(encode(t, b), "", o)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestReadSetFullBodyGolden(t *testing.T) {
	b, o := readSetOptions(t)
	v := readSetValid(t, b, o)
	var g struct {
		ArtifactID string `json:"artifact_id"`
		Digest     string `json:"digest"`
	}
	if e := json.Unmarshal(read(t, "testdata/readset_v2.golden.json"), &g); e != nil {
		t.Fatal(e)
	}
	if v.ArtifactID() != g.ArtifactID || v.Digest() != g.Digest || !bytes.Equal(v.CanonicalBytes(), read(t, "testdata/readset_v2.canonical.json")) {
		t.Fatal("independent Python full body mismatch")
	}
	s := v.Status()
	if s.Structure != "VALID_OFFLINE" || s.Bindings != "PARTIAL_OFFLINE" || s.Qualification != "UNVERIFIABLE_REFERENCE" || s.ActualReads != "NOT_RUN" || s.Authority != "NOT_RUN" {
		t.Fatalf("dishonest status: %+v", s)
	}
	if len(v.PendingDependencies()) < len(o.ContextManifest.PendingDependencies()) {
		t.Fatal("Context pending dependencies lost")
	}
}
func TestReadSetSemanticMutations(t *testing.T) {
	tests := map[string]func(*ReadSetSnapshot, *ReadSetValidationOptions){
		"case":  func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.JournalKey.CaseID = "foreign" },
		"round": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.JournalKey.Round++ },
		"role":  func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.JournalKey.PersonaID = "casper" },
		"manifest": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.JournalKey.ContextManifestID = "ctxm_" + strings.Repeat("f", 64)
		},
		"spec": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.PersonaSpecID = "pspc_" + strings.Repeat("f", 64)
		},
		"experience":         func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.ExperienceSnapshotID = "casper-experience-1" },
		"preference":         func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.UserPreferenceSnapshotID = "foreign" },
		"generation":         func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.ExecutionGeneration++ },
		"context_epoch":      func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.ContextRevocationEpoch++ },
		"authority_scope":    func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.AuthorityScopeID = "foreign" },
		"credential_epoch":   func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.CredentialEpoch++ },
		"index_version":      func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.SourceDependencyIndexVersion = "index-2" },
		"sequence_gap":       func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[1].Sequence++ },
		"sequence_duplicate": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[1].Sequence = 1 },
		"reorder": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.Events[0], b.Events[1] = b.Events[1], b.Events[0]
		},
		"authority_reorder":        func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[1].AuthoritySequence = 89 },
		"foreign_event_generation": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[0].ExecutionGeneration++ },
		"wrong_event_role":         func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[0].TargetPersona = "casper" },
		"crossed_key":              func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[0].JournalKey.Round++ },
		"event_digest": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.Events[0].Source.RawContentDigest = strings.Repeat("f", 64)
		},
		"delivery_conflict": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[1].DeliveryID = b.Events[0].DeliveryID },
		"registered_delivery_conflict": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			e := o.Deliveries[b.Events[0].DeliveryID]
			e.PermissionRef.ObjectID = "foreign"
			o.Deliveries[e.DeliveryID] = e
		},
		"event_source_descriptor": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[0].Source.SourceVersion = "2" },
		"parent_set": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.Events[0].ParentNodeIDs = []string{"node-candidate"}
		},
		"raw_hidden_parent": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.DependencyNodes[0].ParentNodeIDs = []string{"node-shared"}
		},
		"derived_no_parents": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.DependencyNodes[0].Kind = "DERIVED" },
		"orphan": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.DependencyNodes[0].Kind = "DERIVED"
			b.DependencyNodes[0].ParentNodeIDs = []string{"missing"}
		},
		"cycle": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.DependencyNodes[0].Kind = "DERIVED"
			b.DependencyNodes[0].ParentNodeIDs = []string{"node-shared"}
			b.DependencyNodes[1].Kind = "DERIVED"
			b.DependencyNodes[1].ParentNodeIDs = []string{"node-candidate"}
		},
		"missing_dependency": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.DependencyNodes = b.DependencyNodes[:1] },
		"extra_dependency": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			n := b.DependencyNodes[1]
			n.NodeID = "unused"
			b.DependencyNodes = append(b.DependencyNodes, n)
		},
		"source_version_conflict": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.DependencyNodes[1].SourceEntryID = b.DependencyNodes[0].SourceEntryID
			b.DependencyNodes[1].SourceVersion = b.DependencyNodes[0].SourceVersion
		},
		"reused_node_body": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.DependencyNodes[1].NodeID = b.DependencyNodes[0].NodeID
		},
		"fake_source_scope": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			n := o.Sources["node-shared"]
			n.ContextSnapshotID = "casper-context-1"
			o.Sources["node-shared"] = n
		},
		"watermark":               func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.JournalHighWatermark++ },
		"wrong_cutoff":            func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.EvidenceCutoffSequence++ },
		"new_source_after_cutoff": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.Events[1].AuthoritySequence = 101 },
		"late_admission": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			n := o.Sources["node-candidate"]
			n.AdmittedAtSequence = 101
			o.Sources["node-candidate"] = n
		},
		"shared_wrong_kind": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) { b.FinalSharedInputs.Kind = "CONTEXT" },
		"shared_wrong_digest": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.FinalSharedInputs.ContentDigest = strings.Repeat("f", 64)
		},
		"candidate_id": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.CandidateSnapshotID = "csnp_" + strings.Repeat("f", 64)
		},
		"closure_ref": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			b.EvidenceClosureRecordRef.ContentDigest = strings.Repeat("f", 64)
			o.RequireCompleteBindings = true
		},
		"missing_context": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			o.ContextManifest = nil
			o.RequireCompleteBindings = true
		},
		"missing_completeness": func(b *ReadSetSnapshot, o *ReadSetValidationOptions) {
			delete(o.Materials, refKey(b.CompletenessRecordRef))
			o.RequireCompleteBindings = true
		},
	}
	b0, o0 := readSetOptions(t)
	readSetValid(t, b0, o0)
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			b, o := readSetOptions(t)
			change(&b, &o)
			c, e := canonical.Canonicalize(encode(t, b))
			if e != nil {
				t.Fatal(e)
			}
			r := schemaValidator.Validate(readSetContractSchema, c.Bytes())
			if !r.Valid {
				t.Fatalf("must be schema-valid semantic mutation: %+v", r)
			}
			id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
			if _, e = ValidateReadSetSnapshot(c.Bytes(), id, o); e == nil {
				t.Fatal("accepted schema-valid re-addressed semantic violation")
			}
		})
	}
}
func TestReadSetOfflineGateAndMissingDependencies(t *testing.T) {
	b, o := readSetOptions(t)
	for _, gate := range []string{"qualification", "actual", "complete_context"} {
		t.Run(gate, func(t *testing.T) {
			q := o
			switch gate {
			case "qualification":
				q.RequireCompleteQualification = true
			case "actual":
				q.RequireActualReadProof = true
			case "complete_context":
				q.RequireCompleteBindings = true
			}
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", q); e == nil {
				t.Fatal("offline evidence passed strict gate")
			}
		})
	}
	v, e := ValidateReadSetSnapshot(encode(t, b), "", ReadSetValidationOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if v.Status().Bindings != "PARTIAL_OFFLINE" {
		t.Fatal(v.Status())
	}
}
func TestReadSetReadbackLimitsAndCopies(t *testing.T) {
	b, o := readSetOptions(t)
	v := readSetValid(t, b, o)
	raw := v.CanonicalBytes()
	o.Limits.MaxBytes = len(raw)
	if _, e := ReadReadSetSnapshot(raw, v.ArtifactID(), o); e != nil {
		t.Fatal(e)
	}
	env := Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: raw}
	transport := encode(t, env)
	if _, e := ReadReadSetSnapshotEnvelope(transport, v.ArtifactID(), o); e != nil {
		t.Fatal(e)
	}
	for name, run := range map[string]func() error{
		"body_plus1": func() error {
			q := o
			q.Limits.MaxBytes = len(raw) - 1
			_, e := ReadReadSetSnapshotEnvelope(transport, v.ArtifactID(), q)
			return e
		},
		"empty_expected": func() error { _, e := ReadReadSetSnapshot(raw, "", o); return e },
		"wrong_prefix":   func() error { _, e := ReadReadSetSnapshot(raw, "ctxm_"+v.Digest(), o); return e },
		"wrong_digest":   func() error { _, e := ReadReadSetSnapshot(raw, "rset_"+strings.Repeat("f", 64), o); return e },
		"noncanonical": func() error {
			q := o
			q.Limits.MaxBytes++
			_, e := ReadReadSetSnapshot(append(raw, '\n'), v.ArtifactID(), q)
			return e
		},
		"envelope_digest": func() error {
			x := env
			x.Digest = strings.Repeat("f", 64)
			_, e := ReadReadSetSnapshotEnvelope(encode(t, x), v.ArtifactID(), o)
			return e
		},
		"envelope_body_noncanonical": func() error {
			q := o
			q.Limits.MaxBytes++
			bad := append([]byte("{ "), raw[1:]...)
			transport := append([]byte(`{"artifact_id":"`+v.ArtifactID()+`","digest":"`+v.Digest()+`","canonical_profile":"magi-cj/1","digest_alg":"sha256","body":`), append(bad, '}')...)
			_, e := ReadReadSetSnapshotEnvelope(transport, v.ArtifactID(), q)
			return e
		},
		"members":  func() error { q := o; q.Limits.MaxMembers = 10; _, e := ValidateReadSetSnapshot(raw, "", q); return e },
		"depth":    func() error { q := o; q.Limits.MaxDepth = 3; _, e := ValidateReadSetSnapshot(raw, "", q); return e },
		"events":   func() error { q := o; q.MaxEvents = 1; _, e := ValidateReadSetSnapshot(raw, "", q); return e },
		"nodes":    func() error { q := o; q.MaxNodes = 1; _, e := ValidateReadSetSnapshot(raw, "", q); return e },
		"material": func() error { q := o; q.MaxMaterialBytes = 1; _, e := ValidateReadSetSnapshot(raw, "", q); return e },
		"total_material": func() error {
			q := o
			q.MaxTotalMaterialBytes = 1
			_, e := ValidateReadSetSnapshot(raw, "", q)
			return e
		},
	} {
		t.Run(name, func(t *testing.T) {
			if e := run(); e == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	body := v.Body()
	body.Events[0].Source.ParentNodeIDs = append(body.Events[0].Source.ParentNodeIDs, "changed")
	body.DependencyNodes[0].SourceRef.Version = "changed"
	raw[0] = 'x'
	pending := v.PendingDependencies()
	pending[0].Detail = "changed"
	if reflect.DeepEqual(body, v.Body()) || bytes.Equal(raw, v.CanonicalBytes()) || v.PendingDependencies()[0].Detail == "changed" {
		t.Fatal("mutable validated result")
	}
}
func TestReadSetSchemaMirror(t *testing.T) {
	disk, e := os.ReadFile("../../../docs/contracts/decision-artifacts-v2/schema.json")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(disk, readSetContractSchema) {
		t.Fatal("frozen v2 schema mirror drift")
	}
}

// Rehash external declarations to make record mutations semantic, not digest failures.
func mutateReadSetRecord(t *testing.T, b *ReadSetSnapshot, o *ReadSetValidationOptions, ref *ExternalRef, mut func(map[string]any)) {
	t.Helper()
	m := o.Materials[refKey(*ref)]
	delete(o.Materials, refKey(*ref))
	var v map[string]any
	_ = json.Unmarshal(m.RawBytes, &v)
	mut(v)
	m.RawBytes = canonicalTestBytes(t, encode(t, v))
	sum := sha256.Sum256(m.RawBytes)
	ref.ContentDigest = hex.EncodeToString(sum[:])
	m.Reference = *ref
	o.Materials[refKey(*ref)] = m
}
func TestReadSetExternalRecordMutations(t *testing.T) {
	for _, name := range []string{"watermark", "settlement", "events", "closure_cutoff", "closure_prefix", "closure_zero", "receipt_role", "receipt_package", "closure_epoch", "post_closure_replay"} {
		t.Run(name, func(t *testing.T) {
			b, o := readSetOptions(t)
			r := &b.CompletenessRecordRef
			if strings.HasPrefix(name, "closure_") {
				r = &b.EvidenceClosureRecordRef
			}
			if strings.HasPrefix(name, "receipt_") {
				r = &b.FinalSharedDeliveryReceiptRef
			}
			mutateReadSetRecord(t, &b, &o, r, func(m map[string]any) {
				switch name {
				case "watermark":
					m["journal_high_watermark"] = 3
				case "settlement":
					m["delivery_settled"] = false
				case "events":
					m["events"].([]any)[0].(map[string]any)["delivery_id"] = "changed"
				case "closure_cutoff":
					m["evidence_cutoff_sequence"] = 101
				case "closure_prefix":
					m["close_high_watermarks"].(map[string]any)["melchior"] = 3
				case "closure_zero":
					m["close_high_watermarks"].(map[string]any)["casper"] = 0
				case "receipt_role":
					m["journal_key"].(map[string]any)["persona_id"] = "casper"
				case "receipt_package":
					m["final_shared_inputs"].(map[string]any)["snapshot_id"] = "changed"
				case "closure_epoch":
					m["authority_epoch"] = 4
				case "post_closure_replay":
					m["recorded_at"] = "2026-10-22T00:00:04.000Z"
				}
			})
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
				t.Fatal("accepted tampered immutable external declaration")
			}
		})
	}
}
func resealReadSetCompleteness(t *testing.T, b *ReadSetSnapshot, o *ReadSetValidationOptions) {
	t.Helper()
	mutateReadSetRecord(t, b, o, &b.CompletenessRecordRef, func(m map[string]any) {
		var events any
		_ = json.Unmarshal(encode(t, b.Events), &events)
		m["events"] = events
		m["journal_high_watermark"] = b.JournalHighWatermark
	})
}
func TestReadSetLegalRereadAndDerivedClosure(t *testing.T) {
	t.Run("legal_reread", func(t *testing.T) {
		b, o := readSetOptions(t)
		ev := b.Events[0]
		ev.Sequence = 3
		ev.AuthoritySequence = 101
		ev.DeliveryID = "delivery-reread"
		ev.InjectedAt = "2026-10-22T00:00:10.000Z"
		b.Events = append(b.Events, ev)
		b.JournalHighWatermark = 3
		o.Deliveries[ev.DeliveryID] = ev
		resealReadSetCompleteness(t, &b, &o)
		readSetValid(t, b, o)
		t.Run("new_version", func(t *testing.T) {
			b.Events[2].Source.SourceVersion = "2"
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
				t.Fatal("new version treated as reread")
			}
		})
	})
	t.Run("derived", func(t *testing.T) {
		b, o := readSetOptions(t)
		n := b.DependencyNodes[0]
		n.NodeID = "node-derived"
		n.SourceEntryID = "summary-1"
		n.SourceVersion = "build-abcdef"
		n.Kind = "DERIVED"
		n.ParentNodeIDs = []string{"node-candidate", "node-shared"}
		n.SourceRef.ObjectID = "summary-1"
		b.DependencyNodes = append(b.DependencyNodes[:1], append([]SourceNode{n}, b.DependencyNodes[1:]...)...)
		o.Sources[n.NodeID] = ReadSetSourceBinding{Node: n, AdmittedAtSequence: 96}
		ev := b.Events[1]
		ev.Sequence = 3
		ev.AuthoritySequence = 97
		ev.DeliveryID = "summary-delivery"
		ev.Source = n
		ev.ParentNodeIDs = append([]string{}, n.ParentNodeIDs...)
		ev.Kind = "HISTORY_SUMMARY"
		ev.InjectedAt = "2026-10-22T00:00:09.000Z"
		b.Events = append(b.Events, ev)
		b.JournalHighWatermark = 3
		o.Deliveries[ev.DeliveryID] = ev
		resealReadSetCompleteness(t, &b, &o)
		mutateReadSetRecord(t, &b, &o, &b.EvidenceClosureRecordRef, func(m map[string]any) { m["close_high_watermarks"].(map[string]any)["melchior"] = 3 })
		readSetValid(t, b, o)
		t.Run("depth_limit", func(t *testing.T) {
			q := o
			q.MaxGraphDepth = 1
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", q); e == nil {
				t.Fatal("ancestor depth ceiling ignored")
			}
		})
		t.Run("foreign_nested_root", func(t *testing.T) {
			binding := o.Sources["node-shared"]
			binding.ContextSnapshotID = "casper-experience-1"
			o.Sources["node-shared"] = binding
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
				t.Fatal("derived label escaped access ceiling")
			}
		})
	})
}
func TestReadSetVersions(t *testing.T) {
	for _, v := range []string{"build-abcdef", "2026-10-22", "1"} {
		t.Run(v, func(t *testing.T) {
			b, o := readSetOptions(t)
			n := b.DependencyNodes[0]
			n.SourceRef.Version = v
			b.DependencyNodes[0] = n
			b.Events[1].Source = n
			binding := o.Sources[n.NodeID]
			binding.Node = n
			o.Sources[n.NodeID] = binding
			o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
			resealReadSetCompleteness(t, &b, &o)
			readSetValid(t, b, o)
		})
	}
	for _, v := range []string{"latest", "release-CURRENT", "refs-main", "v1.x"} {
		t.Run(v, func(t *testing.T) {
			b, o := readSetOptions(t)
			b.Events[0].PermissionRef.Version = v
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
				t.Fatal("mutable alias accepted")
			}
		})
	}
}
func TestReadSetNestedAdmissionPrecedesExposure(t *testing.T) {
	b, o := readSetOptions(t)
	// The candidate node is read only as an ancestor of a derived source at 95.
	n := b.DependencyNodes[0]
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
	binding := o.Sources["node-candidate"]
	binding.AdmittedAtSequence = 96
	o.Sources["node-candidate"] = binding
	if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
		t.Fatal("ancestor admitted after declared derived exposure")
	}
}
func TestReadSetNoFutureContentReference(t *testing.T) {
	b, o := readSetOptions(t)
	n := b.DependencyNodes[0]
	n.SourceRef.ObjectID = "rset_" + strings.Repeat("a", 64)
	b.DependencyNodes[0] = n
	b.Events[1].Source = n
	binding := o.Sources[n.NodeID]
	binding.Node = n
	o.Sources[n.NodeID] = binding
	o.Deliveries[b.Events[1].DeliveryID] = b.Events[1]
	resealReadSetCompleteness(t, &b, &o)
	if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
		t.Fatal("dependency reference to future ReadSet accepted")
	}
}

// Build a genuinely validated complete-local Context and re-address descendants
// in dependency order; no private validated status is forged by this test.
func readSetCompleteOptions(t *testing.T) (ReadSetSnapshot, ReadSetValidationOptions) {
	t.Helper()
	m := contextFixture(t)
	old := contextFixture(t)
	co := contextCompleteOptions(t, &m)
	ctx, e := ValidateContextManifest(encode(t, m), "", co)
	if e != nil {
		t.Fatal(e)
	}
	var fixture map[string]any
	_ = json.Unmarshal(read(t, "testdata/readset_v2_fixture.json"), &fixture)
	replace := func(before, after string) {
		var walk func(any) any
		walk = func(v any) any {
			switch x := v.(type) {
			case map[string]any:
				for k, c := range x {
					x[k] = walk(c)
				}
			case []any:
				for i, c := range x {
					x[i] = walk(c)
				}
			case string:
				if x == before {
					return after
				}
			}
			return v
		}
		walk(fixture)
	}
	var g struct {
		ArtifactID string `json:"artifact_id"`
	}
	_ = json.Unmarshal(read(t, "testdata/context_manifest.golden.json"), &g)
	replace(g.ArtifactID, ctx.ArtifactID())
	for _, role := range manifestRoles {
		replace(old.Roles[role].PersonaSpecID, m.Roles[role].PersonaSpecID)
	}
	records := fixture["records"].(map[string]any)
	rehash := func(name string) {
		r := records[name].(map[string]any)
		ref := r["ref"].(map[string]any)
		sum := sha256.Sum256(canonicalTestBytes(t, encode(t, r["body"])))
		replace(ref["content_digest"].(string), hex.EncodeToString(sum[:]))
	}
	for _, role := range manifestRoles {
		rehash("receipt." + role)
	}
	cand := fixture["candidate"].(map[string]any)
	c, e := canonical.Canonicalize(encode(t, cand["body"]))
	if e != nil {
		t.Fatal(e)
	}
	newID, _ := c.ArtifactID(canonical.TypeCandidateSnapshot)
	oldBytes, _ := hex.DecodeString(cand["canonical_bytes_hex"].(string))
	oldSum := sha256.Sum256(oldBytes)
	newSum := sha256.Sum256(c.Bytes())
	replace(cand["artifact_id"].(string), newID)
	replace(hex.EncodeToString(oldSum[:]), hex.EncodeToString(newSum[:]))
	rehash("closure")
	rehash("completeness.melchior")
	var f readSetFixture
	if e = json.Unmarshal(encode(t, fixture), &f); e != nil {
		t.Fatal(e)
	}
	return readSetOptionsFor(t, f, ctx)
}
func TestReadSetCompleteLocalBindingsAndMissingInputs(t *testing.T) {
	b, o := readSetCompleteOptions(t)
	o.RequireCompleteBindings = true
	v := readSetValid(t, b, o)
	if v.Status().Bindings != "CHECKED_OFFLINE" || v.Status().Qualification != "UNVERIFIABLE_REFERENCE" || v.Status().ActualReads != "NOT_RUN" || v.Status().Completeness != "UNVERIFIABLE_REFERENCE" {
		t.Fatal(v.Status())
	}
	for _, name := range []string{"selection", "context", "candidate", "index", "delivery", "receipt", "closure", "completeness"} {
		t.Run(name, func(t *testing.T) {
			b, o := readSetCompleteOptions(t)
			o.RequireCompleteBindings = true
			switch name {
			case "selection":
				o.Selection = nil
			case "context":
				o.ContextManifest = nil
			case "candidate":
				o.Candidate = nil
			case "index":
				o.SourceDependencyIndexVersion = ""
			case "delivery":
				delete(o.Deliveries, b.Events[0].DeliveryID)
			case "receipt":
				delete(o.Materials, refKey(b.FinalSharedDeliveryReceiptRef))
			case "closure":
				delete(o.Materials, refKey(b.EvidenceClosureRecordRef))
			case "completeness":
				delete(o.Materials, refKey(b.CompletenessRecordRef))
			}
			if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
				t.Fatal("missing local binding passed complete mode")
			}
		})
	}
}
func TestReadSetEventCountBoundary(t *testing.T) {
	b, o := readSetOptions(t)
	for len(b.Events) < 512 {
		ev := b.Events[0]
		ev.Sequence = int64(len(b.Events)) + 1
		ev.AuthoritySequence = 100 + ev.Sequence
		ev.DeliveryID = "reread-" + fmt.Sprint(ev.Sequence)
		ev.InjectedAt = "2026-10-22T00:00:10.000Z"
		b.Events = append(b.Events, ev)
		o.Deliveries[ev.DeliveryID] = ev
	}
	b.JournalHighWatermark = 512
	resealReadSetCompleteness(t, &b, &o)
	v := readSetValid(t, b, o)
	if _, e := ReadReadSetSnapshot(v.CanonicalBytes(), v.ArtifactID(), o); e != nil {
		t.Fatal(e)
	}
	b.Events = append(b.Events, b.Events[0])
	b.JournalHighWatermark = 513
	if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
		t.Fatal("513 events accepted")
	}
}
func TestReadSetEnvelopeOverflowSafe(t *testing.T) {
	b, o := readSetOptions(t)
	v := readSetValid(t, b, o)
	limit := int(^uint(0) >> 1)
	o.Limits = ResourceLimits{MaxBytes: limit, MaxDepth: limit, MaxMembers: limit, MaxStringBytes: limit}
	env := Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: canonical.Profile, DigestAlg: canonical.DigestAlg, Body: v.CanonicalBytes()}
	if _, e := ReadReadSetSnapshotEnvelope(encode(t, env), v.ArtifactID(), o); e != nil {
		t.Fatal(e)
	}
}
func TestReadSetExternalMetadataResourceLimit(t *testing.T) {
	b, o := readSetOptions(t)
	ref := b.CompletenessRecordRef
	ref.Namespace = strings.Repeat("a", 16385)
	o.Materials[refKey(ref)] = ReadSetMaterial{Reference: ref}
	if _, e := ValidateReadSetSnapshot(encode(t, b), "", o); e == nil {
		t.Fatal("unbounded external metadata accepted")
	}
}
