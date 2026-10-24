package readsetjournal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/inputartifact"
)

var (
	tokenRE        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,190}$`)
	contextIDRE    = regexp.MustCompile(`^ctxm_[0-9a-f]{64}$`)
	digestRE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	utcMillisRE    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}Z$`)
	mutableAliasRE = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(latest|current|stable|main|master|head|default|active|newest|next|x)([^A-Za-z0-9]|$)`)
)

var eventKinds = map[string]struct{}{
	"RETRIEVAL": {}, "TOOL_INJECTION": {}, "HISTORY_SUMMARY": {}, "PREFERENCE": {},
	"EXPERIENCE": {}, "SHARED_FACT": {}, "CACHE_REPLAY": {}, "RESTORE": {}, "DEBATE": {},
}

var eventPhases = map[string]struct{}{
	"INITIAL_REVIEW": {}, "CROSS_EXAMINATION": {}, "FINAL_REVIEW": {},
}

var roles = map[string]struct{}{"melchior": {}, "balthasar": {}, "casper": {}}

// PrepareRegistration performs every pure validation/canonicalization step.
// The adapter must durably commit its output before returning a registration
// permit or allowing provider exposure.
func PrepareRegistration(cfg Config, req RegisterRequest) (PreparedRegistration, error) {
	if !cfg.Enabled {
		return PreparedRegistration{}, ErrDisabled
	}
	cfg = cfg.effective()
	if cfg.MaxEventBytes > 32768 || cfg.MaxEventMembers > 512 || cfg.MaxStringBytes > 8192 ||
		cfg.MaxGraphDepth > 16 || cfg.MaxNodes > 2048 || cfg.MaxParents > 64 ||
		cfg.MaxEventsPerJournal > 512 || cfg.MaxJournalBytes > 16*1024*1024 || cfg.MaxClosureBytes > 1024*1024 {
		return PreparedRegistration{}, limitErr("configuration exceeds frozen ReadSet journal ceilings")
	}
	if err := validateJournalKey(req.Event.JournalKey); err != nil {
		return PreparedRegistration{}, err
	}
	if err := validateOwner(req.Owner, req.Event.JournalKey, req.Event.ExecutionGeneration); err != nil {
		return PreparedRegistration{}, err
	}
	if err := validatePlan(req.Event, cfg); err != nil {
		return PreparedRegistration{}, err
	}
	journalID, err := JournalIDFor(req.Event.JournalKey)
	if err != nil {
		return PreparedRegistration{}, err
	}
	eventCanonical, err := canonical.CanonicalizeStructWithLimits(req.Event, canonical.Limits{
		MaxBytes: cfg.MaxEventBytes, MaxDepth: cfg.MaxGraphDepth,
		MaxMembers: cfg.MaxEventMembers, MaxStringBytes: cfg.MaxStringBytes,
	})
	if err != nil {
		return PreparedRegistration{}, canonicalLimitOrInvalid(err, "registration event")
	}
	if len(eventCanonical.Bytes()) > cfg.MaxEventBytes {
		return PreparedRegistration{}, limitErr("registration event exceeds byte ceiling")
	}
	nodes, depth, err := prepareClosure(req.Event.Source, req.Closure, cfg)
	if err != nil {
		return PreparedRegistration{}, err
	}
	eventDigest := rawDigest(eventCanonical.Bytes())
	nodeDigests := make([]string, len(nodes))
	for i := range nodes {
		nodeDigests[i] = nodes[i].Digest
	}
	identity := struct {
		Protocol    string   `json:"protocol"`
		EventDigest string   `json:"event_digest"`
		NodeDigests []string `json:"node_digests"`
	}{Protocol: Protocol, EventDigest: eventDigest, NodeDigests: nodeDigests}
	identityCanonical, err := canonical.CanonicalizeStructWithLimits(identity, canonical.Limits{
		MaxBytes: cfg.MaxEventBytes, MaxDepth: 4, MaxMembers: cfg.MaxNodes + 4, MaxStringBytes: 128,
	})
	if err != nil {
		return PreparedRegistration{}, canonicalLimitOrInvalid(err, "registration identity")
	}
	return PreparedRegistration{
		JournalID: journalID, JournalKey: req.Event.JournalKey, Scope: req.Event.AdmissionScope,
		Event: req.Event, EventBytes: eventCanonical.Bytes(), EventDigest: eventDigest,
		RequestDigest: rawDigest(identityCanonical.Bytes()), Nodes: nodes, MaxGraphDepth: depth,
	}, nil
}

// JournalIDFor derives a stable local journal identifier from the frozen key.
func JournalIDFor(key JournalKey) (string, error) {
	if err := validateJournalKey(key); err != nil {
		return "", err
	}
	c, err := canonical.CanonicalizeStructWithLimits(key, canonical.Limits{
		MaxBytes: 4096, MaxDepth: 4, MaxMembers: 16, MaxStringBytes: 512,
	})
	if err != nil {
		return "", canonicalLimitOrInvalid(err, "journal key")
	}
	sum := sha256.Sum256([]byte("magi:readset-journal:v1\n" + c.String()))
	return "rsj_" + hex.EncodeToString(sum[:]), nil
}

// AdoptRegistrationEvent requires stored canonical bytes byte-for-byte.
func AdoptRegistrationEvent(raw []byte) ([]byte, error) {
	c, err := canonical.CanonicalizeWithLimits(raw, canonical.Limits{
		MaxBytes: 32768, MaxDepth: 16, MaxMembers: 512, MaxStringBytes: 8192,
	})
	if err != nil {
		return nil, canonicalLimitOrInvalid(err, "stored registration event")
	}
	if !bytes.Equal(raw, c.Bytes()) {
		return nil, fmt.Errorf("%w: stored event is not canonical", ErrInvalidJournal)
	}
	var plan RegistrationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("%w: stored event decode: %v", ErrInvalidJournal, err)
	}
	if err := validatePlan(plan, DefaultConfig().effective()); err != nil {
		return nil, err
	}
	return c.Bytes(), nil
}

// DecodeRegistrationEvent adopts canonical stored bytes and returns their typed plan.
func DecodeRegistrationEvent(raw []byte) (RegistrationPlan, error) {
	adopted, err := AdoptRegistrationEvent(raw)
	if err != nil {
		return RegistrationPlan{}, err
	}
	var plan RegistrationPlan
	if err := json.Unmarshal(adopted, &plan); err != nil {
		return RegistrationPlan{}, fmt.Errorf("%w: %v", ErrInvalidJournal, err)
	}
	return plan, nil
}

// ValidateCurrentScope compares exact scoped identities/epochs. Any difference
// is fail-closed: a read attempt cannot select an older or foreign scope.
func ValidateCurrentScope(admitted, current AdmissionScope) error {
	if admitted != current {
		return fmt.Errorf("%w: admitted context=%s/%d authority=%s/%d credential=%s/%d current context=%s/%d authority=%s/%d credential=%s/%d", ErrRevoked,
			admitted.ContextRevocationScopeID, admitted.ContextRevocationEpoch,
			admitted.AuthorityScopeID, admitted.AuthorityEpoch,
			admitted.CredentialScopeID, admitted.CredentialEpoch,
			current.ContextRevocationScopeID, current.ContextRevocationEpoch,
			current.AuthorityScopeID, current.AuthorityEpoch,
			current.CredentialScopeID, current.CredentialEpoch)
	}
	return nil
}

func validateOwner(owner OwnerFence, key JournalKey, eventGeneration int64) error {
	if owner.CaseID == "" || owner.CaseID != key.CaseID || !validToken(owner.JobID) ||
		!validToken(owner.WorkerID) || owner.ExecutionGeneration <= 0 || owner.ExecutionGeneration > canonical.MaxSafeInteger {
		return fmt.Errorf("%w: durable case/job/worker/positive generation required", ErrOwnerFence)
	}
	if eventGeneration != owner.ExecutionGeneration || eventGeneration <= 0 || eventGeneration > canonical.MaxSafeInteger {
		return fmt.Errorf("%w: event generation %d differs from owner generation %d", ErrOwnerFence, eventGeneration, owner.ExecutionGeneration)
	}
	return nil
}

func validateJournalKey(key JournalKey) error {
	if !validToken(key.CaseID) || len(key.CaseID) > 64 || key.Round < 1 || key.Round > canonical.MaxSafeInteger ||
		!contextIDRE.MatchString(key.ContextManifestID) {
		return fmt.Errorf("%w: invalid case/round/ContextManifest identity", ErrInvalidJournal)
	}
	if _, ok := roles[key.PersonaID]; !ok {
		return fmt.Errorf("%w: invalid role %q", ErrInvalidJournal, key.PersonaID)
	}
	return nil
}

func validatePlan(plan RegistrationPlan, cfg Config) error {
	if plan.EventVersion != EventVersion {
		return fmt.Errorf("%w: unsupported event version %q", ErrInvalidJournal, plan.EventVersion)
	}
	if err := validateJournalKey(plan.JournalKey); err != nil {
		return err
	}
	if plan.ExecutionGeneration <= 0 || plan.ExecutionGeneration > canonical.MaxSafeInteger {
		return fmt.Errorf("%w: positive execution generation required", ErrOwnerFence)
	}
	if !validToken(plan.DeliveryID) || !validToken(plan.ProviderRequestID) {
		return fmt.Errorf("%w: delivery/provider request identities required", ErrInvalidJournal)
	}
	if _, ok := eventKinds[plan.Kind]; !ok {
		return fmt.Errorf("%w: unsupported event kind %q", ErrInvalidJournal, plan.Kind)
	}
	if _, ok := eventPhases[plan.Phase]; !ok {
		return fmt.Errorf("%w: unsupported phase %q", ErrInvalidJournal, plan.Phase)
	}
	if plan.TargetPersona != plan.JournalKey.PersonaID {
		return fmt.Errorf("%w: target persona differs from journal role", ErrInvalidJournal)
	}
	if plan.ParentNodeIDs == nil || plan.Source.ParentNodeIDs == nil {
		return fmt.Errorf("%w: parent arrays must be explicit JSON arrays", ErrInvalidJournal)
	}
	if !slices.Equal(plan.ParentNodeIDs, plan.Source.ParentNodeIDs) {
		return fmt.Errorf("%w: event parents differ from exact source descriptor", ErrDependency)
	}
	if err := validateNode(plan.Source, cfg); err != nil {
		return err
	}
	if err := validateExternalRef(plan.PermissionRef, "permission"); err != nil {
		return err
	}
	if !validToken(plan.Producer.SubjectID) || !validToken(plan.Producer.IdentitySourceID) ||
		!validFixedVersion(plan.Producer.IdentitySourceVersion) {
		return fmt.Errorf("%w: producer identity declaration is incomplete", ErrInvalidJournal)
	}
	return validateScope(plan.AdmissionScope, cfg)
}

func validateScope(scope AdmissionScope, cfg Config) error {
	for name, value := range map[string]string{
		"admission_scope_id":              scope.AdmissionScopeID,
		"source_dependency_index_version": scope.SourceDependencyIndexVersion,
		"context_revocation_scope_id":     scope.ContextRevocationScopeID,
		"authority_scope_id":              scope.AuthorityScopeID,
		"credential_scope_id":             scope.CredentialScopeID,
	} {
		if !validToken(value) {
			return fmt.Errorf("%w: %s is not an exact token", ErrInvalidJournal, name)
		}
	}
	if !validFixedVersion(scope.SourceDependencyIndexVersion) {
		return fmt.Errorf("%w: source dependency index version is mutable", ErrInvalidJournal)
	}
	for name, epoch := range map[string]int64{
		"context_revocation_epoch": scope.ContextRevocationEpoch,
		"authority_epoch":          scope.AuthorityEpoch,
		"credential_epoch":         scope.CredentialEpoch,
	} {
		if epoch < 0 || epoch > canonical.MaxSafeInteger {
			return fmt.Errorf("%w: %s outside safe range", ErrInvalidJournal, name)
		}
	}
	_ = cfg
	return nil
}

func validateNode(node inputartifact.SourceNode, cfg Config) error {
	for _, value := range []string{node.NodeID, node.SourceEntryID, node.SourceVersion, node.RawContentDigest} {
		if len(value) > cfg.MaxStringBytes {
			return limitErr("source node string exceeds ceiling")
		}
	}
	if !validToken(node.NodeID) || !validToken(node.SourceEntryID) || !validFixedVersion(node.SourceVersion) {
		return fmt.Errorf("%w: node/source/version identity is incomplete", ErrInvalidJournal)
	}
	if !digestRE.MatchString(node.RawContentDigest) || node.RawContentDigest != node.SourceRef.ContentDigest {
		return fmt.Errorf("%w: raw content digest is not exact source digest", ErrInvalidJournal)
	}
	if node.ParentNodeIDs == nil {
		return fmt.Errorf("%w: node parent IDs must be an explicit JSON array", ErrInvalidJournal)
	}
	switch node.Kind {
	case "RAW", "EMPTY_RESULT":
		if len(node.ParentNodeIDs) != 0 {
			return fmt.Errorf("%w: %s node cannot have parents", ErrDependency, node.Kind)
		}
	case "DERIVED":
		if len(node.ParentNodeIDs) == 0 {
			return fmt.Errorf("%w: derived node requires parents", ErrDependency)
		}
	default:
		return fmt.Errorf("%w: unsupported source kind %q", ErrInvalidJournal, node.Kind)
	}
	if len(node.ParentNodeIDs) > cfg.MaxParents {
		return limitErr("source parent count exceeds ceiling")
	}
	if !strictlySortedUnique(node.ParentNodeIDs) {
		return fmt.Errorf("%w: parent IDs must be sorted and unique", ErrDependency)
	}
	if err := validateExternalRef(node.SourceRef, "source"); err != nil {
		return err
	}
	if err := validateExternalRef(node.DerivationMethodRef, "derivation_method"); err != nil {
		return err
	}
	if err := validateValidity(node.Validity); err != nil {
		return err
	}
	return nil
}

func validateExternalRef(ref inputartifact.ExternalRef, field string) error {
	if !validToken(ref.Namespace) || !validToken(ref.ObjectID) || !validFixedVersion(ref.Version) ||
		ref.DigestAlg != "sha256" || !digestRE.MatchString(ref.ContentDigest) {
		return fmt.Errorf("%w: %s reference is not exact and immutable", ErrInvalidJournal, field)
	}
	return nil
}

func validateValidity(v inputartifact.Validity) error {
	notBefore, err := parseFrozenTime(v.NotBefore)
	if err != nil {
		return fmt.Errorf("%w: invalid not_before", ErrInvalidJournal)
	}
	if v.ValidUntil != nil {
		until, err := parseFrozenTime(*v.ValidUntil)
		if err != nil || !until.After(notBefore) {
			return fmt.Errorf("%w: invalid valid_until", ErrInvalidJournal)
		}
	}
	return nil
}

func parseFrozenTime(value string) (time.Time, error) {
	if !utcMillisRE.MatchString(value) {
		return time.Time{}, fmt.Errorf("time is not strict UTC milliseconds")
	}
	return time.Parse("2006-01-02T15:04:05.000Z", value)
}

func prepareClosure(source inputartifact.SourceNode, closure []inputartifact.SourceNode, cfg Config) ([]PreparedNode, int, error) {
	if len(closure) == 0 {
		return nil, 0, fmt.Errorf("%w: event source closure is empty", ErrDependency)
	}
	if len(closure) > cfg.MaxNodes {
		return nil, 0, limitErr("dependency node count exceeds ceiling")
	}
	byID := make(map[string]inputartifact.SourceNode, len(closure))
	byVersion := make(map[string]inputartifact.SourceNode, len(closure))
	totalBytes := 0
	nodes := make([]PreparedNode, 0, len(closure))
	for _, node := range closure {
		if err := validateNode(node, cfg); err != nil {
			return nil, 0, err
		}
		if prev, exists := byID[node.NodeID]; exists {
			if reflect.DeepEqual(prev, node) {
				return nil, 0, fmt.Errorf("%w: duplicate node identity %q", ErrDependency, node.NodeID)
			}
			return nil, 0, fmt.Errorf("%w: node ID has conflicting descriptors", ErrDependency)
		}
		versionKey := node.SourceEntryID + "\x00" + node.SourceVersion
		if prev, exists := byVersion[versionKey]; exists && !reflect.DeepEqual(prev, node) {
			return nil, 0, fmt.Errorf("%w: source/version has conflicting nodes", ErrDependency)
		}
		byID[node.NodeID] = node
		byVersion[versionKey] = node
		c, err := canonical.CanonicalizeStructWithLimits(node, canonical.Limits{
			MaxBytes: 32768, MaxDepth: cfg.MaxGraphDepth,
			MaxMembers: cfg.MaxEventMembers, MaxStringBytes: cfg.MaxStringBytes,
		})
		if err != nil {
			return nil, 0, canonicalLimitOrInvalid(err, "dependency node")
		}
		totalBytes += len(c.Bytes())
		if totalBytes > cfg.MaxClosureBytes {
			return nil, 0, limitErr("dependency closure exceeds aggregate byte ceiling")
		}
		nodes = append(nodes, PreparedNode{Node: node, Bytes: c.Bytes(), Digest: rawDigest(c.Bytes())})
	}
	if !reflect.DeepEqual(byID[source.NodeID], source) {
		return nil, 0, fmt.Errorf("%w: event source is absent from exact closure", ErrDependency)
	}
	for _, node := range closure {
		for _, parent := range node.ParentNodeIDs {
			if _, ok := byID[parent]; !ok {
				return nil, 0, fmt.Errorf("%w: missing parent %q for node %q", ErrDependency, parent, node.NodeID)
			}
		}
	}
	visiting := make(map[string]bool, len(byID))
	seen := make(map[string]bool, len(byID))
	depthMemo := make(map[string]int, len(byID))
	var visit func(string, int) (int, error)
	visit = func(id string, depth int) (int, error) {
		if visiting[id] {
			return 0, fmt.Errorf("%w: dependency cycle at %q", ErrDependency, id)
		}
		if depth > cfg.MaxGraphDepth {
			return 0, limitErr("dependency graph depth exceeds ceiling")
		}
		if seen[id] {
			return depthMemo[id], nil
		}
		node, ok := byID[id]
		if !ok {
			return 0, fmt.Errorf("%w: missing node %q", ErrDependency, id)
		}
		visiting[id] = true
		longest := 1
		for _, parent := range node.ParentNodeIDs {
			d, err := visit(parent, depth+1)
			if err != nil {
				return 0, err
			}
			if d+1 > longest {
				longest = d + 1
			}
		}
		visiting[id] = false
		seen[id] = true
		depthMemo[id] = longest
		return longest, nil
	}
	maxDepth, err := visit(source.NodeID, 1)
	if err != nil {
		return nil, 0, err
	}
	if len(seen) != len(byID) {
		return nil, 0, fmt.Errorf("%w: closure contains node outside event source ancestry", ErrDependency)
	}
	slices.SortFunc(nodes, func(a, b PreparedNode) int { return strings.Compare(a.Node.NodeID, b.Node.NodeID) })
	return nodes, maxDepth, nil
}

func validToken(s string) bool { return tokenRE.MatchString(s) }

func validFixedVersion(s string) bool {
	return validToken(s) && !mutableAliasRE.MatchString(s)
}

func strictlySortedUnique(values []string) bool {
	for i, value := range values {
		if !validToken(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func rawDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func limitErr(detail string) error { return fmt.Errorf("%w: %s", ErrLimit, detail) }

func canonicalLimitOrInvalid(err error, field string) error {
	var ce *canonical.Error
	if errors.As(err, &ce) {
		switch ce.Kind {
		case canonical.KindPayloadTooLarge, canonical.KindTooDeep, canonical.KindTooManyMembers, canonical.KindStringTooLong:
			return fmt.Errorf("%w: %s: %s", ErrLimit, field, ce.Error())
		}
	}
	return fmt.Errorf("%w: %s: %v", ErrInvalidJournal, field, err)
}

// ValidateJournalGraph verifies the persisted read-time graph: every event
// source exists exactly, the edge table exactly equals every node's declared
// parents, every node belongs to an event ancestor closure, and the complete
// graph is acyclic. It never promotes provider delivery state.
func ValidateJournalGraph(plans []RegistrationPlan, nodes []inputartifact.SourceNode, edges []DependencyEdge) error {
	if len(plans) > 512 || len(nodes) > 2048 {
		return limitErr("persisted graph count exceeds ceiling")
	}
	if len(edges) > len(nodes)*64 {
		return limitErr("persisted edge count exceeds ceiling")
	}
	cfg := DefaultConfig().effective()
	byID := make(map[string]inputartifact.SourceNode, len(nodes))
	for _, node := range nodes {
		if err := validateNode(node, cfg); err != nil {
			return err
		}
		if _, exists := byID[node.NodeID]; exists {
			return fmt.Errorf("%w: duplicate persisted node identity", ErrDependency)
		}
		byID[node.NodeID] = node
	}
	expectedEdges := make(map[string]struct{})
	actualEdges := make(map[string]struct{}, len(edges))
	for _, node := range nodes {
		for _, parent := range node.ParentNodeIDs {
			expectedEdges[parent+"\x00"+node.NodeID] = struct{}{}
		}
	}
	for _, edge := range edges {
		if _, ok := byID[edge.ParentNodeID]; !ok {
			return fmt.Errorf("%w: reverse edge has missing parent", ErrDependency)
		}
		if _, ok := byID[edge.ChildNodeID]; !ok {
			return fmt.Errorf("%w: reverse edge has missing child", ErrDependency)
		}
		key := edge.ParentNodeID + "\x00" + edge.ChildNodeID
		if _, duplicate := actualEdges[key]; duplicate {
			return fmt.Errorf("%w: duplicate reverse edge", ErrDependency)
		}
		actualEdges[key] = struct{}{}
	}
	if !reflect.DeepEqual(expectedEdges, actualEdges) {
		return fmt.Errorf("%w: persisted dependency edges differ from node parents", ErrDependency)
	}
	visiting, visited := make(map[string]bool, len(nodes)), make(map[string]bool, len(nodes))
	depthMemo := make(map[string]int, len(nodes))
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if visiting[id] {
			return fmt.Errorf("%w: persisted dependency cycle", ErrDependency)
		}
		if depth > cfg.MaxGraphDepth {
			return limitErr("persisted dependency depth exceeds ceiling")
		}
		if visited[id] {
			return nil
		}
		node, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: persisted dependency has missing ancestor", ErrDependency)
		}
		visiting[id] = true
		for _, parent := range node.ParentNodeIDs {
			if err := visit(parent, depth+1); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		depthMemo[id] = depth
		return nil
	}
	reachable := make(map[string]bool, len(nodes))
	var mark func(string)
	mark = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, parent := range byID[id].ParentNodeIDs {
			mark(parent)
		}
	}
	for _, plan := range plans {
		if err := validatePlan(plan, cfg); err != nil {
			return err
		}
		if err := visit(plan.Source.NodeID, 1); err != nil {
			return err
		}
		stored, ok := byID[plan.Source.NodeID]
		if !ok || !reflect.DeepEqual(stored, plan.Source) {
			return fmt.Errorf("%w: event source is absent or differs from persisted node", ErrDependency)
		}
		mark(plan.Source.NodeID)
	}
	if len(reachable) != len(byID) {
		return fmt.Errorf("%w: persisted orphan node outside all event closures", ErrDependency)
	}
	return nil
}
