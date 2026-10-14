package execution_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
)

// C0 canonical-encoding protection: the digests below are already persisted and
// re-compared at runtime (checkpoints, approvals). The values are frozen exactly
// as produced before C0 was introduced; see docs/canonical-encoding-contract.md
// §2 for the algorithm ids.
//
// If one of these fails, the legacy algorithm was changed. That is forbidden:
// it would invalidate existing checkpoints/approvals instead of migrating them.
// Do not "fix" this test by updating the constants.
func TestLegacyManifestDigestsAreByteFrozen(t *testing.T) {
	t.Run("legacy-approval-intent/v1", func(t *testing.T) {
		if got := execution.ApprovalIntentDigest("calc", []byte(`{"a":1,"b":2}`)); got != "97690d62bb6dc3695e3f5e0c0c523145baf24deb7d8f61d00b9013fcad2d32d9" {
			t.Fatalf("ApprovalIntentDigest changed: %s", got)
		}
		if got := execution.ToolIdempotencyKey("inv-1", "calc", []byte(`{"a":1}`)); got != "0aafc97acd91a2328b45c7576eb6a7d84e4ceb96c4eb0c4d3aad73e9b42173cb" {
			t.Fatalf("ToolIdempotencyKey changed: %s", got)
		}
	})
	t.Run("legacy-manifest/v1", func(t *testing.T) {
		if got := execution.ModelReferenceDigest(entity.ModelRef{ModelID: 7, ModelName: "m", BaseURL: "u"}); got != "d7aa41e8c5cbb618c31a6effdf7d442afa6cc5acc7ce29ed2af056902b084432" {
			t.Fatalf("ModelReferenceDigest changed: %s", got)
		}
		m := execution.FreezeManifest(entity.RunEnvironment{
			RuntimeVersion: "1.0", PromptVersion: "p1", ModelName: "m", ModelBaseURL: "u",
			Tools: []string{"calc", "search"}, ConfigVersion: 1,
		})
		for name, got := range map[string]struct{ got, want string }{
			"ModelDigest":    {m.ModelDigest, "ee32b9c19cdb8b7f3c387ae7c8e1a8d04857542fc22bbf367523eb89e24b2b7d"},
			"ToolsetDigest":  {m.ToolsetDigest, "ba1ecd0bb20b0a62d936a49658949fd694b81aebacc079d740edaca851060a43"},
			"ConfigDigest":   {m.ConfigDigest, "94b553d42113e5bbe75029d3706a39ada47db59211c04770bf167fad260b4317"},
			"ManifestDigest": {m.ManifestDigest, "08015f47c2cb220d202e2a78f093b9fcd4c3c780e08483dc9985e49e9a1048f4"},
		} {
			if got.got != got.want {
				t.Fatalf("%s changed: %s", name, got.got)
			}
		}
	})
}
