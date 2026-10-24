// Offline structure and C0 identity oracle only. No protocol runtime or writes.
// Called by check-decision-artifact-v2.py from backend, using existing validators.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/inputartifact"
	"github.com/jamespud/magi/backend/domain/validation"
)

type probe struct {
	FixedVersion *string         `json:"fixed_version"`
	RiskInput    json.RawMessage `json:"risk_input"`
	Body         json.RawMessage `json:"body"`
	ID           string          `json:"artifact_id"`
	Digest       string          `json:"digest"`
	CanonicalHex string          `json:"canonical_bytes_hex"`
	Profile      string          `json:"canonical_profile"`
	Algorithm    string          `json:"digest_alg"`
}

// Reuse the frozen, private fixedVersion rule through its existing public
// consumer; no new production export, copied Go rule, or SemVer approximation.
func fixedVersionProbe(version string) string {
	raw, err := os.ReadFile("domain/inputartifact/testdata/domain_profile.json")
	if err != nil {
		panic(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		panic(err)
	}
	var ref map[string]json.RawMessage
	if err := json.Unmarshal(body["governance_policy_ref"], &ref); err != nil {
		panic(err)
	}
	ref["version"], err = json.Marshal(version)
	if err != nil {
		panic(err)
	}
	body["governance_policy_ref"], err = json.Marshal(ref)
	if err != nil {
		panic(err)
	}
	changed, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	_, err = inputartifact.ValidateDomainProfile(changed, "", inputartifact.ValidationOptions{})
	if err != nil {
		var failure *inputartifact.Error
		if errors.As(err, &failure) && failure.Kind == "VERSION" {
			return "VERSION"
		}
		panic(err)
	}
	return ""
}

func main() {
	schema, err := os.ReadFile("../docs/contracts/decision-artifacts-v2/schema.json")
	if err != nil {
		panic(err)
	}
	var probes []probe
	if err := json.NewDecoder(os.Stdin).Decode(&probes); err != nil {
		panic(err)
	}
	validator := validation.NewJSONSchemaValidator()
	results := make([]string, len(probes))
	for i, p := range probes {
		if p.FixedVersion != nil {
			results[i] = fixedVersionProbe(*p.FixedVersion)
			continue
		}
		if p.RiskInput != nil {
			c, err := canonical.Canonicalize(p.RiskInput)
			if err != nil {
				panic(err)
			}
			digest := sha256.Sum256(append([]byte("magi:decision-risk-input:v1\n"), c.Bytes()...))
			if hex.EncodeToString(digest[:]) != p.Digest || hex.EncodeToString(c.Bytes()) != p.CanonicalHex {
				results[i] = "RISK_INPUT"
			}
			continue
		}
		var header struct {
			Type canonical.ArtifactType `json:"artifact_type"`
		}
		if err := json.Unmarshal(p.Body, &header); err != nil {
			panic(err)
		}
		limits := canonical.Limits{MaxBytes: 262144, MaxDepth: 24, MaxMembers: 4096, MaxStringBytes: 16384}
		if header.Type == canonical.TypeReadSetSnapshot {
			limits.MaxBytes = 1048576
			limits.MaxMembers = 32768
		}
		c, err := canonical.CanonicalizeWithLimits(p.Body, limits)
		if err != nil {
			results[i] = "C0"
			continue
		}
		validation := validator.Validate(schema, c.Bytes())
		if !validation.Valid {
			for _, v := range validation.Violations {
				if len(v.Code) >= 7 && v.Code[:7] == "SCHEMA_" {
					panic(fmt.Sprintf("invalid schema: %+v", validation))
				}
			}
			results[i] = "SCHEMA"
			continue
		}
		// External records do not pretend to be new C0 artifact types.
		if header.Type == "" {
			continue
		}
		digest, err := c.Digest(header.Type)
		if err != nil {
			results[i] = "IDENTITY"
			continue
		}
		id, err := c.ArtifactID(header.Type)
		if err != nil || id != p.ID || digest != p.Digest || p.Profile != "magi-cj/1" || p.Algorithm != "sha256" {
			results[i] = "IDENTITY"
			continue
		}
		raw, err := hex.DecodeString(p.CanonicalHex)
		if err != nil {
			results[i] = "IDENTITY"
			continue
		}
		adopted, err := canonical.Adopt(raw)
		if err != nil || !bytes.Equal(raw, c.Bytes()) || !bytes.Equal(adopted.Bytes(), c.Bytes()) {
			results[i] = "ADOPT"
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}
