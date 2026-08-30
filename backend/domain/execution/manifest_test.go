package execution

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestManifestDigestIndependentOfToolOrdering(t *testing.T) {
	tools := []string{"web:search", "system:clock", "knowledge:query"}
	first := FreezeManifest(entity.RunEnvironment{
		ModelName:      "model-a",
		ModelBaseURL:   "https://models.example.test/v1",
		Tools:          tools,
		KnowledgeIndex: true,
		ConfigVersion:  12,
		RuntimeVersion: "runtime-v1",
		PromptVersion:  "prompt-v1",
	})
	second := FreezeManifest(entity.RunEnvironment{
		ModelName:      "model-a",
		ModelBaseURL:   "https://models.example.test/v1",
		Tools:          []string{"knowledge:query", "web:search", "system:clock"},
		KnowledgeIndex: true,
		ConfigVersion:  12,
		RuntimeVersion: "runtime-v1",
		PromptVersion:  "prompt-v1",
	})

	if first.ManifestDigest != second.ManifestDigest {
		t.Fatalf("manifest digest changed with tool ordering: first=%q second=%q", first.ManifestDigest, second.ManifestDigest)
	}
	if !reflect.DeepEqual(tools, []string{"web:search", "system:clock", "knowledge:query"}) {
		t.Fatalf("FreezeManifest mutated caller-owned tools: %v", tools)
	}
}

func TestManifestDigestChangesWhenPromptChanges(t *testing.T) {
	base := entity.RunEnvironment{
		ModelName:      "model-a",
		ModelBaseURL:   "https://models.example.test/v1",
		Tools:          []string{"web:search"},
		KnowledgeIndex: true,
		ConfigVersion:  12,
		RuntimeVersion: "runtime-v1",
		PromptVersion:  "prompt-v1",
	}
	changed := base
	changed.PromptVersion = "prompt-v2"

	if FreezeManifest(base).ManifestDigest == FreezeManifest(changed).ManifestDigest {
		t.Fatal("manifest digest did not change when prompt version changed")
	}
}

func TestManifestDigestChangesWhenModelChanges(t *testing.T) {
	base := entity.RunEnvironment{
		ModelName:      "model-a",
		ModelBaseURL:   "https://models.example.test/v1",
		Tools:          []string{"web:search"},
		KnowledgeIndex: true,
		ConfigVersion:  12,
		RuntimeVersion: "runtime-v1",
		PromptVersion:  "prompt-v1",
	}
	changed := base
	changed.ModelName = "model-b"

	if FreezeManifest(base).ManifestDigest == FreezeManifest(changed).ManifestDigest {
		t.Fatal("manifest digest did not change when model changed")
	}
}

func TestValidateManifestAllowsMatchingNonEmptyDigests(t *testing.T) {
	if err := ValidateManifest("digest-a", "digest-a"); err != nil {
		t.Fatalf("ValidateManifest() error = %v, want nil", err)
	}
}

func TestValidateManifestRejectsMissingOrMismatchedDigest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkpoint string
		current    string
	}{
		{name: "both missing", checkpoint: "", current: ""},
		{name: "missing checkpoint", checkpoint: "", current: "digest-a"},
		{name: "missing current", checkpoint: "digest-a", current: ""},
		{name: "mismatch", checkpoint: "digest-a", current: "digest-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateManifest(tc.checkpoint, tc.current)
			if !errors.Is(err, ErrManifestMismatch) {
				t.Fatalf("ValidateManifest(%q, %q) error = %v, want ErrManifestMismatch", tc.checkpoint, tc.current, err)
			}
		})
	}
}
