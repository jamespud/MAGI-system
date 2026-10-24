package readsetjournal

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/inputartifact"
)

func digestOf(ch byte) string { return strings.Repeat(string(ch), 64) }

func validRegisterRequest() RegisterRequest {
	key := JournalKey{
		CaseID:            "case-readset-journal",
		Round:             1,
		ContextManifestID: "ctxm_" + digestOf('a'),
		PersonaID:         "melchior",
	}
	source := inputartifact.SourceNode{
		NodeID:           "node-raw-1",
		SourceEntryID:    "source-entry-1",
		SourceVersion:    "1",
		RawContentDigest: digestOf('b'),
		Kind:             "RAW",
		SourceRef: inputartifact.ExternalRef{
			Namespace: "test", ObjectID: "source-entry-1", Version: "1",
			DigestAlg: "sha256", ContentDigest: digestOf('b'),
		},
		Validity: inputartifact.Validity{NotBefore: "2026-10-24T00:00:00.000Z"},
		DerivationMethodRef: inputartifact.ExternalRef{
			Namespace: "test", ObjectID: "raw-method", Version: "1",
			DigestAlg: "sha256", ContentDigest: digestOf('c'),
		},
		ParentNodeIDs: []string{},
	}
	scope := AdmissionScope{
		AdmissionScopeID:             "admission-scope-1",
		SourceDependencyIndexVersion: "index-1",
		ContextRevocationScopeID:     "context-scope-1",
		ContextRevocationEpoch:       0,
		AuthorityScopeID:             "authority-scope-1",
		AuthorityEpoch:               0,
		CredentialScopeID:            "credential-scope-1",
		CredentialEpoch:              0,
	}
	return RegisterRequest{
		Owner: OwnerFence{
			CaseID: key.CaseID, JobID: "job-1", WorkerID: "worker-1", ExecutionGeneration: 7,
		},
		Event: RegistrationPlan{
			EventVersion:        EventVersion,
			JournalKey:          key,
			ExecutionGeneration: 7,
			DeliveryID:          "delivery-1",
			ProviderRequestID:   "provider-request-1",
			Kind:                "RETRIEVAL",
			Phase:               "INITIAL_REVIEW",
			Source:              source,
			ParentNodeIDs:       []string{},
			PermissionRef: inputartifact.ExternalRef{
				Namespace: "test", ObjectID: "permission-1", Version: "1",
				DigestAlg: "sha256", ContentDigest: digestOf('d'),
			},
			Producer: inputartifact.SubjectRef{
				SubjectID: "producer-1", IdentitySourceID: "identity-root-1", IdentitySourceVersion: "1",
			},
			TargetPersona:  "melchior",
			AdmissionScope: scope,
		},
		Closure: []inputartifact.SourceNode{source},
	}
}

func enabledConfig() Config {
	c := DefaultConfig()
	c.Enabled = true
	return c
}

func TestDefaultConfigIsDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("readset journal must default off")
	}
	if _, err := PrepareRegistration(cfg, validRegisterRequest()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("PrepareRegistration disabled error = %v, want ErrDisabled", err)
	}
}

func TestPrepareRegistrationIsDeterministicAndCanonical(t *testing.T) {
	req := validRegisterRequest()
	first, err := PrepareRegistration(enabledConfig(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareRegistration(enabledConfig(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.JournalID == "" || first.JournalID != second.JournalID {
		t.Fatalf("journal IDs differ: %q %q", first.JournalID, second.JournalID)
	}
	if !bytes.Equal(first.EventBytes, second.EventBytes) || first.EventDigest != second.EventDigest || first.RequestDigest != second.RequestDigest {
		t.Fatal("same registration did not produce exact canonical bytes/digests")
	}
	canonical, err := AdoptRegistrationEvent(first.EventBytes)
	if err != nil {
		t.Fatalf("prepared bytes are not canonical: %v", err)
	}
	if !bytes.Equal(canonical, first.EventBytes) {
		t.Fatal("AdoptRegistrationEvent changed bytes")
	}
	if first.Event.DeliveryID != req.Event.DeliveryID || first.Event.JournalKey != req.Event.JournalKey {
		t.Fatalf("prepared event lost exact identity: %+v", first.Event)
	}
	changed := req
	changed.Event.DeliveryID = "delivery-2"
	third, err := PrepareRegistration(enabledConfig(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if third.JournalID != first.JournalID || third.EventDigest == first.EventDigest || third.RequestDigest == first.RequestDigest {
		t.Fatal("delivery ID mutation was not bound into the event/request digest")
	}
}

func TestOwnerFenceKeepsGenerationSeparateFromScopedEpochs(t *testing.T) {
	req := validRegisterRequest()
	req.Owner.ExecutionGeneration = 0
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrOwnerFence) {
		t.Fatalf("zero owner generation error = %v", err)
	}
	req = validRegisterRequest()
	req.Event.ExecutionGeneration = req.Owner.ExecutionGeneration + 1
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrOwnerFence) {
		t.Fatalf("generation mismatch error = %v", err)
	}
	req = validRegisterRequest()
	req.Event.AdmissionScope.ContextRevocationEpoch = 0
	req.Event.AdmissionScope.AuthorityEpoch = 0
	req.Event.AdmissionScope.CredentialEpoch = 0
	if _, err := PrepareRegistration(enabledConfig(), req); err != nil {
		t.Fatalf("zero scoped epochs must remain legal and distinct from generation: %v", err)
	}
}

func TestDependencyClosureRejectsMissingParentsOrphansAndCycles(t *testing.T) {
	t.Run("derived_without_parents", func(t *testing.T) {
		req := validRegisterRequest()
		req.Event.Source.Kind = "DERIVED"
		req.Event.Source.ParentNodeIDs = []string{}
		req.Event.ParentNodeIDs = []string{}
		req.Closure = []inputartifact.SourceNode{req.Event.Source}
		if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrDependency) {
			t.Fatalf("error = %v, want ErrDependency", err)
		}
	})
	t.Run("missing_parent", func(t *testing.T) {
		req := validRegisterRequest()
		req.Event.Source.Kind = "DERIVED"
		req.Event.Source.ParentNodeIDs = []string{"node-parent"}
		req.Event.ParentNodeIDs = []string{"node-parent"}
		if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrDependency) {
			t.Fatalf("error = %v, want ErrDependency", err)
		}
	})
	t.Run("orphan", func(t *testing.T) {
		req := validRegisterRequest()
		orphan := req.Event.Source
		orphan.NodeID = "node-orphan"
		orphan.SourceEntryID = "source-orphan"
		req.Closure = append(req.Closure, orphan)
		if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrDependency) {
			t.Fatalf("error = %v, want ErrDependency", err)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		req := validRegisterRequest()
		a := req.Event.Source
		a.NodeID, a.SourceEntryID = "node-a", "source-a"
		a.Kind = "DERIVED"
		a.ParentNodeIDs = []string{"node-b"}
		b := req.Event.Source
		b.NodeID, b.SourceEntryID = "node-b", "source-b"
		b.Kind = "DERIVED"
		b.ParentNodeIDs = []string{"node-a"}
		req.Event.Source = a
		req.Event.ParentNodeIDs = append([]string(nil), a.ParentNodeIDs...)
		req.Closure = []inputartifact.SourceNode{a, b}
		if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrDependency) {
			t.Fatalf("error = %v, want ErrDependency", err)
		}
	})
}

func TestDependencyClosureAcceptsClosedDAGAndSortsNodes(t *testing.T) {
	req := validRegisterRequest()
	parent := req.Event.Source
	parent.NodeID, parent.SourceEntryID = "node-parent", "source-parent"
	child := req.Event.Source
	child.NodeID, child.SourceEntryID = "node-child", "source-child"
	child.Kind = "DERIVED"
	child.SourceRef.ObjectID = "source-child"
	child.ParentNodeIDs = []string{"node-parent"}
	req.Event.Source = child
	req.Event.ParentNodeIDs = []string{"node-parent"}
	req.Closure = []inputartifact.SourceNode{child, parent}
	prepared, err := PrepareRegistration(enabledConfig(), req)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(prepared.Nodes))
	for i, node := range prepared.Nodes {
		ids[i] = node.Node.NodeID
	}
	if !slices.Equal(ids, []string{"node-child", "node-parent"}) {
		t.Fatalf("prepared nodes = %v, want stable node-id order", ids)
	}
	if prepared.MaxGraphDepth < 2 {
		t.Fatalf("depth = %d, want at least 2", prepared.MaxGraphDepth)
	}
}

func TestResourceBoundsRejectRatherThanTruncate(t *testing.T) {
	req := validRegisterRequest()
	req.Event.Source.SourceVersion = strings.Repeat("v", 9000)
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize string error = %v, want ErrLimit", err)
	}
	req = validRegisterRequest()
	cfg := enabledConfig()
	cfg.MaxNodes = 0
	parent := req.Event.Source
	parent.NodeID, parent.SourceEntryID = "node-parent", "source-parent"
	child := req.Event.Source
	child.NodeID, child.SourceEntryID = "node-child", "source-child"
	child.Kind = "DERIVED"
	child.SourceRef.ObjectID = "source-child"
	child.ParentNodeIDs = []string{"node-parent"}
	req.Event.Source = child
	req.Event.ParentNodeIDs = []string{"node-parent"}
	req.Closure = []inputartifact.SourceNode{child, parent}
	cfg.MaxNodes = 1
	if _, err := PrepareRegistration(cfg, req); !errors.Is(err, ErrLimit) {
		t.Fatalf("node count error = %v, want ErrLimit", err)
	}
}

func TestCurrentScopeRejectsPostRevocationReplay(t *testing.T) {
	admitted := validRegisterRequest().Event.AdmissionScope
	if err := ValidateCurrentScope(admitted, admitted); err != nil {
		t.Fatalf("exact scope rejected: %v", err)
	}
	revoked := admitted
	revoked.ContextRevocationEpoch++
	if err := ValidateCurrentScope(admitted, revoked); !errors.Is(err, ErrRevoked) {
		t.Fatalf("context revocation error = %v, want ErrRevoked", err)
	}
	foreign := admitted
	foreign.CredentialScopeID = "credential-scope-2"
	if err := ValidateCurrentScope(admitted, foreign); !errors.Is(err, ErrRevoked) {
		t.Fatalf("credential scope change error = %v, want ErrRevoked", err)
	}
	revoked = admitted
	revoked.AuthorityEpoch++
	if err := ValidateCurrentScope(admitted, revoked); !errors.Is(err, ErrRevoked) {
		t.Fatalf("authority revocation error = %v, want ErrRevoked", err)
	}
}

func TestJournalGraphRejectsMissingEdgesAndOrphanNodes(t *testing.T) {
	req := validRegisterRequest()
	plan := req.Event
	parent := plan.Source
	child := plan.Source
	child.NodeID = "node-child"
	child.SourceEntryID = "source-child"
	child.Kind = "DERIVED"
	child.SourceRef.ObjectID = "source-child"
	child.ParentNodeIDs = []string{parent.NodeID}
	plan.Source = child
	plan.ParentNodeIDs = []string{parent.NodeID}
	plans := []RegistrationPlan{plan}
	nodes := []inputartifact.SourceNode{parent, child}
	edges := []DependencyEdge{{ParentNodeID: parent.NodeID, ChildNodeID: child.NodeID}}
	if err := ValidateJournalGraph(plans, nodes, edges); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
	if err := ValidateJournalGraph(plans, nodes, nil); !errors.Is(err, ErrDependency) {
		t.Fatalf("missing edge error = %v", err)
	}
	orphan := parent
	orphan.NodeID = "node-orphan"
	orphan.SourceEntryID = "source-orphan"
	if err := ValidateJournalGraph(plans, append(nodes, orphan), edges); !errors.Is(err, ErrDependency) {
		t.Fatalf("orphan node error = %v", err)
	}
}

func TestDependencyClosureRejectsDuplicateExactNode(t *testing.T) {
	req := validRegisterRequest()
	req.Closure = append(req.Closure, req.Closure[0])
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrDependency) {
		t.Fatalf("duplicate node error = %v", err)
	}
}

func TestRegistrationRequiresFrozenArrayAndUTCTimeShapes(t *testing.T) {
	req := validRegisterRequest()
	req.Event.ParentNodeIDs = nil
	req.Event.Source.ParentNodeIDs = nil
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrInvalidJournal) {
		t.Fatalf("nil parent arrays error = %v, want ErrInvalidJournal", err)
	}
	req = validRegisterRequest()
	req.Event.Source.Validity.NotBefore = "2026-10-24T08:00:00+08:00"
	if _, err := PrepareRegistration(enabledConfig(), req); !errors.Is(err, ErrInvalidJournal) {
		t.Fatalf("non-UTC validity error = %v, want ErrInvalidJournal", err)
	}
}
