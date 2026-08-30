package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ErrManifestMismatch indicates that a resume request does not use the exact
// execution manifest recorded by its checkpoint.
var ErrManifestMismatch = errors.New("execution manifest mismatch")

// FreezeManifest returns an immutable-by-convention execution manifest with
// deterministic digests. It copies and sorts Tools, leaving the caller's
// RunEnvironment and backing slice unchanged.
func FreezeManifest(environment entity.RunEnvironment) entity.RunEnvironment {
	frozen := environment
	frozen.Tools = append([]string{}, environment.Tools...)
	sort.Strings(frozen.Tools)

	frozen.ModelDigest = canonicalDigest("manifest:model:v1", modelFingerprint{
		ModelName:    frozen.ModelName,
		ModelBaseURL: frozen.ModelBaseURL,
	})
	frozen.ToolsetDigest = canonicalDigest("manifest:toolset:v1", toolsetFingerprint{
		Tools: frozen.Tools,
	})
	frozen.ConfigDigest = canonicalDigest("manifest:config:v1", configFingerprint{
		KnowledgeIndex: frozen.KnowledgeIndex,
		ConfigVersion:  frozen.ConfigVersion,
	})
	frozen.ManifestDigest = canonicalDigest("manifest:v1", manifestFingerprint{
		RuntimeVersion: frozen.RuntimeVersion,
		PromptVersion:  frozen.PromptVersion,
		ModelDigest:    frozen.ModelDigest,
		ToolsetDigest:  frozen.ToolsetDigest,
		ConfigDigest:   frozen.ConfigDigest,
	})
	return frozen
}

// ValidateManifest permits resume only when both non-empty manifest digests
// are identical. Missing digests are rejected so legacy checkpoints cannot
// silently bypass manifest validation.
func ValidateManifest(checkpointDigest string, currentDigest string) error {
	if checkpointDigest == "" || currentDigest == "" || checkpointDigest != currentDigest {
		return fmt.Errorf("%w: checkpoint=%q current=%q", ErrManifestMismatch, checkpointDigest, currentDigest)
	}
	return nil
}

type modelFingerprint struct {
	ModelName    string `json:"model_name"`
	ModelBaseURL string `json:"model_base_url"`
}

type toolsetFingerprint struct {
	Tools []string `json:"tools"`
}

type configFingerprint struct {
	KnowledgeIndex bool  `json:"knowledge_index"`
	ConfigVersion  int64 `json:"config_version"`
}

type manifestFingerprint struct {
	RuntimeVersion string `json:"runtime_version"`
	PromptVersion  string `json:"prompt_version"`
	ModelDigest    string `json:"model_digest"`
	ToolsetDigest  string `json:"toolset_digest"`
	ConfigDigest   string `json:"config_digest"`
}

func canonicalDigest(prefix string, value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal manifest fingerprint: %v", err))
	}
	return digest(prefix + "|" + string(encoded))
}
