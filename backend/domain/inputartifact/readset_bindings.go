package inputartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamespud/magi/backend/domain/canonical"
	"reflect"
	"slices"
	"strings"
)

type readSetCandidateBody struct {
	CaseID                         string                 `json:"case_id"`
	Round                          int64                  `json:"round"`
	ContextManifestID              string                 `json:"context_manifest_id"`
	CharterID                      string                 `json:"charter_id"`
	DomainProfileID                string                 `json:"domain_profile_id"`
	Version                        string                 `json:"version"`
	FrozenAt                       string                 `json:"frozen_at"`
	Validity                       Validity               `json:"validity"`
	FinalSharedInputs              SnapshotRef            `json:"final_shared_inputs"`
	PersonaViews                   map[string]RoleView    `json:"persona_views"`
	FinalSharedDeliveryReceiptRefs map[string]ExternalRef `json:"final_shared_delivery_receipt_refs"`
	CandidateCount                 int64                  `json:"candidate_count"`
	CandidateEntries               int64                  `json:"candidate_entries"`
	Candidates                     []json.RawMessage      `json:"candidates"`
}

func readSetScopes(b ReadSetSnapshot) ReadSetSelection {
	return ReadSetSelection{JournalKey: b.JournalKey, ExecutionGeneration: b.ExecutionGeneration, ContextRevocationScopeID: b.ContextRevocationScopeID, ContextRevocationEpoch: b.ContextRevocationEpoch, AuthorityScopeID: b.AuthorityScopeID, AuthorityEpoch: b.AuthorityEpoch, CredentialScopeID: b.CredentialScopeID, CredentialEpoch: b.CredentialEpoch}
}

// Equality is scoped: identical bare epoch integers never imply shared authority.
func readSetSameScopes(b ReadSetSnapshot, v any) bool {
	bv := reflect.ValueOf(b)
	vv := reflect.ValueOf(v)
	for _, name := range []string{"ExecutionGeneration", "ContextRevocationScopeID", "ContextRevocationEpoch", "AuthorityScopeID", "AuthorityEpoch", "CredentialScopeID", "CredentialEpoch"} {
		if !reflect.DeepEqual(bv.FieldByName(name).Interface(), vv.FieldByName(name).Interface()) {
			return false
		}
	}
	return true
}
func checkReadSetBindings(b ReadSetSnapshot, o ReadSetValidationOptions) ([]Dependency, ReadSetValidationStatus, error) {
	status := ReadSetValidationStatus{Structure: "VALID_OFFLINE", Bindings: "CHECKED_OFFLINE", ContextBindings: "UNVERIFIABLE_REFERENCE", Completeness: "UNVERIFIABLE_REFERENCE", Qualification: "UNVERIFIABLE_REFERENCE", ActualReads: "NOT_RUN", Authority: "NOT_RUN"}
	var pending []Dependency
	missing := false
	unresolved := func(path, detail string) {
		pending = append(pending, Dependency{Kind: "UNVERIFIABLE_REFERENCE", Path: path, Detail: detail})
	}
	unavailable := func(path, detail string) {
		missing = true
		status.Bindings = "PARTIAL_OFFLINE"
		unresolved(path, detail)
	}
	fail := func(path, detail string) ([]Dependency, ReadSetValidationStatus, error) {
		return nil, ReadSetValidationStatus{}, invalid("BINDING", path, detail)
	}
	if o.Selection == nil {
		unavailable("$.selection", "independent journal/generation/scoped-epoch selection unavailable")
	} else if *o.Selection != readSetScopes(b) {
		return fail("$.selection", "journal/generation/scoped epochs must equal exact supplied selection")
	}
	var manifest *ContextManifest
	if o.ContextManifest == nil {
		unavailable("$.journal_key.context_manifest_id", "validated offline Manifest unavailable")
	} else {
		ctx := o.ContextManifest
		if ctx.ArtifactID() == "" || ctx.ArtifactID() != b.JournalKey.ContextManifestID {
			return fail("$.journal_key.context_manifest_id", "nonzero validated Manifest must match exact journal identity")
		}
		m := ctx.Body()
		manifest = &m
		view, ok := m.Roles[b.JournalKey.PersonaID]
		if !ok || m.CaseID != b.JournalKey.CaseID || m.Round != b.JournalKey.Round || view.PersonaSpecID != b.PersonaSpecID || view.ExperienceSnapshotID != b.ExperienceSnapshotID || m.UserPreferenceSnapshotID != b.UserPreferenceSnapshotID || m.SourceDependencyIndexVersion != b.SourceDependencyIndexVersion || m.ContextRevocationScopeID != b.ContextRevocationScopeID || m.ContextRevocationEpoch != b.ContextRevocationEpoch {
			return fail("$.journal_key", "Manifest case/round/persona/spec/experience/preference/index/context scope+epoch mismatch")
		}
		status.ContextBindings = ctx.Status().Bindings
		if ctx.Status().SnapshotBytes != "CHECKED_OFFLINE" {
			status.ContextBindings = "PARTIAL_OFFLINE"
		}
		if ctx.Status().Bindings != "CHECKED_OFFLINE" || ctx.Status().SnapshotBytes != "CHECKED_OFFLINE" {
			unavailable("$.context_manifest", "Manifest partial local evidence remains partial; no status promotion")
		}
		for _, d := range ctx.PendingDependencies() {
			d.Path = "$.context_manifest" + strings.TrimPrefix(d.Path, "$")
			pending = append(pending, d)
		}
		frozen, _ := parseTime(b.FrozenAt, "$.frozen_at")
		mf, _ := parseTime(m.FrozenAt, "$.context_manifest.frozen_at")
		if frozen.Before(mf) {
			return fail("$.frozen_at", "ReadSet cannot predate Manifest freeze")
		}
		for _, ev := range b.Events {
			at, _ := parseTime(ev.InjectedAt, "$.events.injected_at")
			if at.Before(mf) {
				return fail("$.events", "event cannot predate Manifest freeze")
			}
			if e := readSetPoint(m.Validity, at, "$.context_manifest.validity"); e != nil {
				return nil, ReadSetValidationStatus{}, e
			}
		}
	}
	// Bound every externally supplied container before traversing or decoding it.
	if !boundedReadSet(len(o.Sources), o.MaxNodes, 2048) || !boundedReadSet(len(o.Deliveries), o.MaxEvents, 512) || len(o.Materials) > 4096 {
		return nil, ReadSetValidationStatus{}, invalid("RESOURCE_LIMIT", "$.bindings", "external registry count limit exceeded")
	}
	per, total := o.MaxMaterialBytes, o.MaxTotalMaterialBytes
	if per <= 0 {
		per = 1048576
	}
	if total <= 0 {
		total = 4194304
	}
	remaining := total
	charge := func(raw []byte) error {
		if len(raw) > per || len(raw) > remaining {
			return invalid("RESOURCE_LIMIT", "$.materials", "per-material or aggregate byte ceiling exceeded")
		}
		remaining -= len(raw)
		return nil
	}
	if o.Candidate != nil {
		if e := charge(o.Candidate.RawBytes); e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
	}
	for key, m := range o.Materials {
		// Bound caller-supplied metadata before refKey concatenates strings.
		for _, text := range []string{key, m.Reference.Namespace, m.Reference.ObjectID, m.Reference.Version, m.Reference.DigestAlg, m.Reference.ContentDigest} {
			if len(text) > 16384 {
				return nil, ReadSetValidationStatus{}, invalid("RESOURCE_LIMIT", "$.materials", "external metadata string ceiling exceeded")
			}
		}
		if key != refKey(m.Reference) {
			return fail("$.materials", "material registry key differs from exact reference")
		}
		if e := charge(m.RawBytes); e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		if m.RawBytes != nil {
			s := sha256.Sum256(m.RawBytes)
			if hex.EncodeToString(s[:]) != m.Reference.ContentDigest {
				return fail("$.materials", "original external bytes digest mismatch")
			}
		}
	}
	kinds := map[string]string{}
	material := func(ref ExternalRef, kind string, dst any) (bool, error) {
		key := refKey(ref)
		if prior, ok := kinds[key]; ok && prior != kind {
			return false, invalid("REFERENCE", "$.materials", "same immutable reference reused across record kinds")
		}
		kinds[key] = kind
		m, ok := o.Materials[key]
		if !ok || m.RawBytes == nil {
			unavailable("$.materials."+ref.ObjectID, "original "+kind+" bytes unavailable; no authority claim")
			return false, nil
		}
		c, e := canonical.CanonicalizeWithLimits(m.RawBytes, readSetLimits(o.Limits))
		if e != nil {
			return false, canonicalError(e)
		}
		if e = readSetSchema(c.Bytes(), kind); e != nil {
			return false, e
		}
		deps, depErr := pendingArtifactReferences(json.RawMessage(c.Bytes()), b.CreatedBy)
		pending = append(pending, deps...)
		if e = depErr; e != nil {
			return false, e
		}
		if e = json.Unmarshal(c.Bytes(), dst); e != nil {
			return false, e
		}
		return true, nil
	}
	var candidate *readSetCandidateBody
	if o.Candidate == nil {
		unavailable("$.candidate_snapshot_id", "immutable selected Candidate bytes unavailable")
	} else {
		v := o.Candidate
		c, e := canonical.CanonicalizeWithLimits(v.RawBytes, readSetLimits(o.Limits))
		if e != nil {
			return nil, ReadSetValidationStatus{}, canonicalError(e)
		}
		if !slices.Equal(v.RawBytes, c.Bytes()) {
			return fail("$.candidate_snapshot_id", "stored Candidate original bytes are not canonical")
		}
		if e = readSetSchema(c.Bytes(), "CandidateSnapshot"); e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		id, _ := c.ArtifactID("candidate_snapshot")
		if id != v.ArtifactID || id != b.CandidateSnapshotID {
			return fail("$.candidate_snapshot_id", "Candidate C0 identity mismatch")
		}
		var cb readSetCandidateBody
		_ = json.Unmarshal(c.Bytes(), &cb)
		candidate = &cb
		if cb.CaseID != b.JournalKey.CaseID || cb.Round != b.JournalKey.Round || cb.ContextManifestID != b.JournalKey.ContextManifestID || !reflect.DeepEqual(cb.FinalSharedInputs, b.FinalSharedInputs) || cb.FinalSharedDeliveryReceiptRefs[b.JournalKey.PersonaID] != b.FinalSharedDeliveryReceiptRef || cb.CandidateCount != int64(len(cb.Candidates)) || cb.CandidateEntries != int64(len(cb.Candidates)) {
			return fail("$.candidate_snapshot_id", "Candidate journal/package/receipt/count declarations mismatch")
		}
		if manifest != nil && (cb.CharterID != manifest.CharterID || cb.DomainProfileID != manifest.DomainProfileID || !reflect.DeepEqual(cb.PersonaViews, manifest.Roles)) {
			return fail("$.candidate_snapshot_id", "Candidate does not bind Manifest domain/Charter/all role views")
		}
		cf, e := parseTime(cb.FrozenAt, "$.candidate.frozen_at")
		if e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		rf, _ := parseTime(b.FrozenAt, "$.frozen_at")
		if cf.After(rf) {
			return fail("$.candidate.frozen_at", "Candidate must preexist ReadSet")
		}
		if e = readSetPoint(cb.Validity, cf, "$.candidate.validity"); e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		deps, depErr := pendingArtifactReferences(json.RawMessage(c.Bytes()), b.CreatedBy)
		pending = append(pending, deps...)
		if e = depErr; e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
	}
	allowed := map[string]SnapshotRef{}
	if manifest != nil {
		view := manifest.Roles[b.JournalKey.PersonaID]
		ids := append([]string{manifest.SharedFactSnapshotID, manifest.UserPreferenceSnapshotID, view.ExperienceSnapshotID}, view.AllowedContextIDs...)
		for _, s := range manifest.SnapshotRefs {
			if slices.Contains(ids, s.SnapshotID) {
				allowed[s.SnapshotID] = s
			}
		}
	}
	nodes := map[string]SourceNode{}
	for _, n := range b.DependencyNodes {
		nodes[n.NodeID] = n
	}
	if o.SourceDependencyIndexVersion == "" {
		unavailable("$.sources.version", "independent immutable dependency-index version unavailable")
	} else if o.SourceDependencyIndexVersion != b.SourceDependencyIndexVersion {
		return fail("$.sources.version", "external dependency-index version differs from Manifest and ReadSet")
	}
	type exposure struct {
		authority   int64
		first, last string
	}
	exposures := map[string]exposure{}
	for _, ev := range b.Events {
		seen := map[string]bool{}
		var expose func(string)
		expose = func(id string) {
			if seen[id] {
				return
			}
			seen[id] = true
			v := exposures[id]
			if v.authority == 0 {
				v.authority = ev.AuthoritySequence
				v.first = ev.InjectedAt
			}
			v.last = ev.InjectedAt
			exposures[id] = v
			for _, parent := range nodes[id].ParentNodeIDs {
				expose(parent)
			}
		}
		expose(ev.Source.NodeID)
	}
	for id := range o.Sources {
		if _, ok := nodes[id]; !ok {
			return fail("$.sources", "dependency index contains extra substituted node outside exact closure")
		}
	}
	for _, n := range b.DependencyNodes {
		binding, ok := o.Sources[n.NodeID]
		if !ok {
			unavailable("$.sources."+n.NodeID, "exact external dependency-index node/membership/admission declaration unavailable")
			continue
		}
		if !reflect.DeepEqual(binding.Node, n) || binding.AdmittedAtSequence < 1 || binding.AdmittedAtSequence > b.EvidenceCutoffSequence {
			return fail("$.sources."+n.NodeID, "index descriptor must match exactly and preregister node before cutoff")
		}
		if binding.AdmittedAtSequence > exposures[n.NodeID].authority {
			return fail("$.sources", "node or ancestor admission occurs after declared exposure")
		}
		// Recursively every node must point into this role's allowed snapshot set;
		// DERIVED cannot hide a foreign ancestor behind an innocuous event kind.
		if n.Kind != "DERIVED" || binding.ContextSnapshotID != "" {
			if binding.ContextSnapshotID == b.CandidateSnapshotID {
				if n.SourceEntryID != b.CandidateSnapshotID || candidate == nil {
					if candidate == nil {
						unavailable("$.sources", "Candidate raw-byte binding unavailable")
					} else {
						return fail("$.sources", "fake Candidate membership")
					}
				} else {
					s := sha256.Sum256(o.Candidate.RawBytes)
					if n.RawContentDigest != hex.EncodeToString(s[:]) || n.SourceVersion != candidate.Version {
						return fail("$.sources", "Candidate source exact version/raw digest mismatch")
					}
				}
			} else if manifest != nil {
				s, ok := allowed[binding.ContextSnapshotID]
				if !ok {
					return fail("$.sources", "raw or nested source membership escapes Manifest role access ceiling")
				}
				for _, stamp := range []string{exposures[n.NodeID].first, exposures[n.NodeID].last} {
					at, _ := parseTime(stamp, "$.events.injected_at")
					if e := readSetPoint(s.Validity, at, "$.sources.snapshot.validity"); e != nil {
						return nil, ReadSetValidationStatus{}, e
					}
				}
				if n.SourceEntryID == s.SnapshotID && (n.SourceVersion != s.Version || n.RawContentDigest != s.ContentDigest || n.SourceRef != s.SourceRef || !reflect.DeepEqual(n.Validity, s.Validity)) {
					return fail("$.sources", "exact snapshot source descriptor differs from Manifest")
				}
			} else {
				unavailable("$.sources", "Manifest access ceiling unavailable")
			}
		}
	}
	for _, ev := range b.Events {
		registered, ok := o.Deliveries[ev.DeliveryID]
		if !ok {
			unavailable("$.deliveries."+ev.DeliveryID, "external registered full event unavailable")
		} else if !reflect.DeepEqual(registered, ev) {
			return fail("$.deliveries", "same delivery ID must retain exact JournalKey and complete event bytes")
		}
		switch ev.Kind {
		case "EXPERIENCE":
			if ev.Source.SourceEntryID != b.ExperienceSnapshotID {
				return fail("$.events", "experience event must use selected role experience")
			}
		case "PREFERENCE":
			if ev.Source.SourceEntryID != b.UserPreferenceSnapshotID {
				return fail("$.events", "preference event must use selected preference")
			}
		case "SHARED_FACT":
			if ev.Source.SourceEntryID != b.FinalSharedInputs.SnapshotID || ev.Source.SourceVersion != b.FinalSharedInputs.Version || ev.Source.RawContentDigest != b.FinalSharedInputs.ContentDigest || ev.Source.SourceRef != b.FinalSharedInputs.SourceRef {
				return fail("$.events", "shared event must use exact final package")
			}
		}
	}
	for id := range o.Deliveries {
		found := false
		for _, ev := range b.Events {
			if ev.DeliveryID == id {
				found = true
			}
		}
		if !found {
			return fail("$.deliveries", "registered delivery absent from supplied finite event sequence")
		}
	}
	var closure EvidenceClosureRecord
	hasClosure, e := material(b.EvidenceClosureRecordRef, "EvidenceClosureRecord", &closure)
	if e != nil {
		return nil, ReadSetValidationStatus{}, e
	}
	if hasClosure {
		if closure.CaseID != b.JournalKey.CaseID || closure.Round != b.JournalKey.Round || closure.ContextManifestID != b.JournalKey.ContextManifestID || closure.CandidateSnapshotID != b.CandidateSnapshotID || !readSetSameScopes(b, closure) || closure.EvidenceCutoffSequence != b.EvidenceCutoffSequence || !reflect.DeepEqual(closure.FinalSharedInputs, b.FinalSharedInputs) || closure.ReceiptRefs[b.JournalKey.PersonaID] != b.FinalSharedDeliveryReceiptRef {
			return fail("$.evidence_closure_record_ref", "closure journal/candidate/package/scoped epochs/cutoff/own receipt mismatch")
		}
		if manifest != nil && !reflect.DeepEqual(closure.PersonaViews, manifest.Roles) {
			return fail("$.evidence_closure_record_ref", "closure role views mismatch")
		}
		h := closure.CloseHighWatermarks[b.JournalKey.PersonaID]
		if h < 1 || h > int64(len(b.Events)) {
			return fail("$.evidence_closure_record_ref", "closed local prefix exceeds finite journal")
		}
		for _, ev := range b.Events {
			if ev.Sequence <= h && ev.AuthoritySequence > b.EvidenceCutoffSequence || ev.Sequence > h && ev.AuthoritySequence <= b.EvidenceCutoffSequence {
				return fail("$.evidence_closure_record_ref", "close high watermark must exactly delimit cutoff prefix")
			}
		}
		ct, e := parseTime(closure.CommittedAt, "$.closure.committed_at")
		if e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		rf, _ := parseTime(b.FrozenAt, "$.frozen_at")
		if ct.After(rf) {
			return fail("$.closure.committed_at", "closure declaration must preexist ReadSet")
		}
		if candidate != nil {
			cf, _ := parseTime(candidate.FrozenAt, "$.candidate.frozen_at")
			if ct.Before(cf) {
				return fail("$.closure.committed_at", "closure declaration predates candidate")
			}
		}
	}
	// Verify all three supplied receipt declarations when referenced; missing
	// other-role bytes remain explicitly partial. They never establish delivery.
	receiptRefs := map[string]ExternalRef{b.JournalKey.PersonaID: b.FinalSharedDeliveryReceiptRef}
	if candidate != nil {
		receiptRefs = candidate.FinalSharedDeliveryReceiptRefs
	}
	if hasClosure && candidate != nil && !reflect.DeepEqual(closure.ReceiptRefs, candidate.FinalSharedDeliveryReceiptRefs) {
		return fail("$.closure.receipt_refs", "closure receipts differ from immutable Candidate")
	}
	for _, role := range manifestRoles {
		ref, ok := receiptRefs[role]
		if !ok {
			unavailable("$.receipts."+role, "other-role receipt reference unavailable")
			continue
		}
		var r TrustedDeliveryReceipt
		present, e := material(ref, "TrustedDeliveryReceipt", &r)
		if e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		if !present {
			continue
		}
		key := b.JournalKey
		key.PersonaID = role
		if r.JournalKey != key || !readSetSameScopes(b, r) || r.UserPreferenceSnapshotID != b.UserPreferenceSnapshotID || !reflect.DeepEqual(r.FinalSharedInputs, b.FinalSharedInputs) {
			return fail("$.receipts."+role, "receipt exact key/package/scoped declarations mismatch")
		}
		if manifest != nil {
			view := manifest.Roles[role]
			if r.PersonaSpecID != view.PersonaSpecID || r.ExperienceSnapshotID != view.ExperienceSnapshotID {
				return fail("$.receipts."+role, "receipt role view mismatch")
			}
		}
		if hasClosure && r.QuiescenceFenceID != closure.QuiescenceFenceID {
			return fail("$.receipts", "quiescence fence mismatch")
		}
		verified, e := parseTime(r.VerifiedAt, "$.receipt.verified_at")
		if e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		if candidate != nil {
			cf, _ := parseTime(candidate.FrozenAt, "$.candidate.frozen_at")
			if verified.After(cf) {
				return fail("$.receipts", "receipt must preexist Candidate")
			}
		}
		if role == b.JournalKey.PersonaID {
			if r.PersonaSpecID != b.PersonaSpecID || r.ExperienceSnapshotID != b.ExperienceSnapshotID || r.RegisteredInjectionSequence < 1 || r.RegisteredInjectionSequence > int64(len(b.Events)) {
				return fail("$.receipts", "own receipt spec/experience/injection sequence mismatch")
			}
			ev := b.Events[r.RegisteredInjectionSequence-1]
			at, _ := parseTime(ev.InjectedAt, "$.event.injected_at")
			if ev.Kind != "SHARED_FACT" || ev.Source.SourceEntryID != b.FinalSharedInputs.SnapshotID || ev.AuthoritySequence > b.EvidenceCutoffSequence || verified.Before(at) {
				return fail("$.receipts", "own registered delivery must bind a pre-cutoff shared event before verification")
			}
		}
	}
	var comp CompletenessRecord
	hasComp, e := material(b.CompletenessRecordRef, "CompletenessRecord", &comp)
	if e != nil {
		return nil, ReadSetValidationStatus{}, e
	}
	if hasComp {
		if comp.JournalKey != b.JournalKey || comp.JournalHighWatermark != b.JournalHighWatermark || !reflect.DeepEqual(comp.Events, b.Events) {
			return fail("$.completeness_record_ref", "completeness declaration must retain exact full event version sequence and high watermark")
		}
		at, e := parseTime(comp.RecordedAt, "$.completeness.recorded_at")
		if e != nil {
			return nil, ReadSetValidationStatus{}, e
		}
		last, _ := parseTime(b.Events[len(b.Events)-1].InjectedAt, "$.events.injected_at")
		rf, _ := parseTime(b.FrozenAt, "$.frozen_at")
		if at.Before(last) || at.After(rf) {
			return fail("$.completeness_record_ref", "settlement declaration must follow all supplied events and preexist ReadSet")
		}
	}
	unresolved("$.completeness_record_ref", "consistent synthetic settlement/COMMITTED declarations are untrusted; provider/Journal completeness and signed issuer/permission provenance require C1-C2B/C2/C3")
	unresolved("$.sources", "index membership, producer origin and raw/derived chain authentication are declarations only; source authorization and revocation unknown")
	unresolved("$.events", "actual model exposure, independent witness and authoritative order allocator NOT RUN")
	if missing && o.RequireCompleteBindings {
		return nil, ReadSetValidationStatus{}, invalid("UNVERIFIABLE_REFERENCE", "$", "complete local bindings unavailable; partial Context and missing immutable evidence fail closed")
	}
	return pending, status, nil
}
