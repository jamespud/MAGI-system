package canonical_test

import (
	"regexp"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

// These are identity probes, not valid C1-C ReadSetSnapshot bodies. C0 does not
// validate body schemas, publication eligibility, dependency closure or ballots.
func TestReadSetSnapshotIdentity(t *testing.T) {
	typ := canonical.TypeReadSetSnapshot
	base, err := canonical.Canonicalize([]byte(`{"artifact_type":"read_set_snapshot","context_revocation_epoch":0,"schema_version":"1.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("registered_type_and_prefix", func(t *testing.T) {
		if !canonical.ValidType(typ) {
			t.Fatal("read_set_snapshot must be a registered C0 type")
		}
		id, err := base.ArtifactID(typ)
		if err != nil || !regexp.MustCompile(`^rset_[0-9a-f]{64}$`).MatchString(id) {
			t.Fatalf("invalid ReadSet identity %q: %v", id, err)
		}
	})
	t.Run("unknown_type_rejected", func(t *testing.T) {
		for _, unknown := range []canonical.ArtifactType{"read_set_journal", "read_set_snapshot\nx"} {
			if canonical.ValidType(unknown) {
				t.Fatalf("unknown type %q accepted", unknown)
			}
			if _, err := base.Digest(unknown); kindOf(t, err) != canonical.KindUnknownType {
				t.Fatalf("wrong digest rejection for %q: %v", unknown, err)
			}
			if _, err := base.ArtifactID(unknown); kindOf(t, err) != canonical.KindUnknownType {
				t.Fatalf("wrong ID rejection for %q: %v", unknown, err)
			}
		}
	})
	t.Run("same_body_different_types", func(t *testing.T) {
		rset, err := base.Digest(typ)
		if err != nil {
			t.Fatal(err)
		}
		for _, other := range []canonical.ArtifactType{canonical.TypeContextManifest, canonical.TypeSealedBallot} {
			digest, err := base.Digest(other)
			if err != nil || digest == rset {
				t.Fatalf("ReadSet domain not separated from %s: %v", other, err)
			}
		}
	})
	t.Run("equivalent_encodings_same_identity", func(t *testing.T) {
		other, err := canonical.Canonicalize([]byte(`{ "schema_version": "1.0", "context_revocation_epoch": 0, "artifact_type": "read_set_\u0073napshot" }`))
		if err != nil {
			t.Fatal(err)
		}
		want, err := base.ArtifactID(typ)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := other.ArtifactID(typ); err != nil || got != want {
			t.Fatalf("equivalent encoding identity %q (%v), want %q", got, err, want)
		}
		adopted, err := canonical.Adopt(base.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if got, err := adopted.ArtifactID(typ); err != nil || got != want {
			t.Fatalf("adopted identity %q (%v), want %q", got, err, want)
		}
	})
	t.Run("noncanonical_storage_rejected", func(t *testing.T) {
		if _, err := canonical.Adopt([]byte(`{ "artifact_type": "read_set_snapshot", "context_revocation_epoch": 0, "schema_version": "1.0" }`)); kindOf(t, err) != canonical.KindNonCanonical {
			t.Fatalf("non-canonical bytes accepted: %v", err)
		}
	})
	t.Run("authoritative_field_changes_digest", func(t *testing.T) {
		want, err := base.Digest(typ)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{
			`{"artifact_type":"read_set_snapshot","context_revocation_epoch":1,"schema_version":"1.0"}`,
			`{"artifact_type":"read_set_snapshot","context_revocation_epoch":0,"schema_version":"1.1"}`,
		} {
			changed, err := canonical.Canonicalize([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if got, err := changed.Digest(typ); err != nil || got == want {
				t.Fatalf("field mutation did not change identity: %v", err)
			}
		}
	})
}

func TestRegisteredArtifactIDsAreByteFrozen(t *testing.T) {
	// Every pre-C0-R type plus ReadSet has an independently computed fixed ID
	// for identical {"a":1} bytes in golden_vectors.json.
	types := []canonical.ArtifactType{
		canonical.TypeDecisionCharter, canonical.TypeDomainProfile, canonical.TypePersonaSpec,
		canonical.TypeContextManifest, canonical.TypeCandidateSnapshot, canonical.TypeSealedBallot,
		canonical.TypeBallotSet, canonical.TypeDecisionRecord, canonical.TypeAuthorityProof,
		canonical.TypeActionIntent, canonical.TypeAuditReceipt, canonical.TypeSendRecord,
		canonical.TypeAcceptanceManifest, canonical.TypeReadSetSnapshot,
	}
	vectors := loadVectors(t)
	for _, typ := range types {
		t.Run(string(typ), func(t *testing.T) {
			for _, v := range vectors.Positive {
				if v.Name != "identity_"+string(typ) {
					continue
				}
				if v.ArtifactType != string(typ) || v.ArtifactID == "" {
					t.Fatal("missing typed golden ID")
				}
				c, err := canonical.Adopt([]byte(`{"a":1}`))
				if err != nil {
					t.Fatal(err)
				}
				id, err := c.ArtifactID(typ)
				if err != nil || id != v.ArtifactID {
					t.Fatalf("registered ID changed: got %q (%v), want %q", id, err, v.ArtifactID)
				}
				return
			}
			t.Fatal("missing registered-type regression vector")
		})
	}
}
