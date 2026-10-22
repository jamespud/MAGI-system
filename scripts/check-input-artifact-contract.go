// Run from backend: go run ../scripts/check-input-artifact-contract.go
// This checks the C1-A document's structural examples using existing packages.
// It deliberately does not implement publication or cross-artifact validators.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/validation"
)

type example struct {
	Name string         `json:"name"`
	Body map[string]any `json:"body"`
}

type mutation struct {
	name string
	edit func(map[string]any)
}

type policyCase struct {
	mutation
	valid bool
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func read(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	return b
}

func encode(value any) []byte {
	b, err := json.Marshal(value)
	if err != nil {
		fail("encode fixture: %v", err)
	}
	return b
}

func clone(value map[string]any) map[string]any {
	var copied map[string]any
	if err := json.Unmarshal(encode(value), &copied); err != nil {
		fail("clone fixture: %v", err)
	}
	return copied
}

func main() {
	dir := "../docs/contracts/input-artifacts"
	if len(os.Args) == 2 {
		dir = os.Args[1]
	} else if len(os.Args) != 1 {
		fail("usage: check-input-artifact-contract [fixture-directory]")
	}
	schema := read(filepath.Join(dir, "schema.json"))
	var examples []example
	if err := json.Unmarshal(read(filepath.Join(dir, "examples.json")), &examples); err != nil {
		fail("parse examples: %v", err)
	}
	if len(examples) != 5 {
		fail("expected domain + three personas + context, got %d examples", len(examples))
	}
	validator := validation.NewJSONSchemaValidator()
	positive, negative, identity := 0, 0, 0
	policyChecks := 0
	for _, ex := range examples {
		limits := canonical.Limits{MaxBytes: 262144, MaxDepth: 24, MaxMembers: 4096, MaxStringBytes: 16384}
		if ex.Body["artifact_type"] == "persona_spec" {
			limits.MaxBytes = 131072
		}
		c, err := canonical.CanonicalizeWithLimits(encode(ex.Body), limits)
		if err != nil {
			fail("%s: C0: %v", ex.Name, err)
		}
		if result := validator.Validate(schema, c.Bytes()); result == nil || !result.Valid {
			fail("%s: structural schema: %+v", ex.Name, result)
		}
		positive++
		typ := canonical.ArtifactType(ex.Body["artifact_type"].(string))
		digest, err := c.Digest(typ)
		if err != nil {
			fail("%s: digest: %v", ex.Name, err)
		}
		id, err := c.ArtifactID(typ)
		if err != nil {
			fail("%s: id: %v", ex.Name, err)
		}
		adopted, err := canonical.Adopt(c.Bytes())
		if err != nil || !bytes.Equal(adopted.Bytes(), c.Bytes()) {
			fail("%s: canonical storage adoption failed: %v", ex.Name, err)
		}
		if got, err := adopted.ArtifactID(typ); err != nil || got != id {
			fail("%s: adoption identity mismatch: %v", ex.Name, err)
		}
		mutated := clone(ex.Body)
		mutated["version"] = "1.0.1"
		changed, err := canonical.CanonicalizeWithLimits(encode(mutated), limits)
		if err != nil {
			fail("%s: mutation: %v", ex.Name, err)
		}
		if result := validator.Validate(schema, changed.Bytes()); result == nil || !result.Valid {
			fail("%s: version mutation must remain structurally valid", ex.Name)
		}
		if got, err := changed.Digest(typ); err != nil || got == digest {
			fail("%s: authoritative version mutation did not change digest: %v", ex.Name, err)
		}
		identity++
		// D-04: a generic DomainProfile describes budgets independently of
		// Persona v1.0 and ROLLOUT-LAB instance validation. This is structural
		// coverage only; accepting a value does not authorize its use.
		var policyCases []policyCase
		switch typ {
		case canonical.TypeDomainProfile:
			for _, tc := range []struct {
				group, field string
				value        int
				valid        bool
			}{
				{"budget", "role_tool_calls", 12, true},
				{"budget", "role_output_tokens", 12000, true},
				{"windows", "read_retries", 2, true},
				{"windows", "format_repairs_per_role", 2, true},
				{"budget", "role_tool_calls", 0, false},
				{"budget", "role_output_tokens", 0, false},
				{"windows", "read_retries", -1, false},
				{"windows", "format_repairs_per_role", -1, false},
			} {
				policyCases = append(policyCases, policyCase{mutation{fmt.Sprintf("%s=%d", tc.field, tc.value), func(b map[string]any) {
					b[tc.group].(map[string]any)[tc.field] = tc.value
				}}, tc.valid})
			}
		case canonical.TypePersonaSpec:
			policyCases = append(policyCases, policyCase{mutation{"persona-tool-budget-raised", func(b map[string]any) {
				b["tool_call_limit"] = 7
			}}, false}, policyCase{mutation{"persona-output-budget-raised", func(b map[string]any) {
				b["output_token_limit"] = 6001
			}}, false})
		}
		for _, tc := range policyCases {
			body := clone(ex.Body)
			tc.edit(body)
			parsed, err := canonical.CanonicalizeWithLimits(encode(body), limits)
			if err != nil {
				fail("%s/D-04/%s: C0: %v", ex.Name, tc.name, err)
			}
			result := validator.Validate(schema, parsed.Bytes())
			if result == nil || result.Valid != tc.valid {
				fail("%s/D-04/%s: expected structural validity %t, got %+v", ex.Name, tc.name, tc.valid, result)
			}
			policyChecks++
		}
		mutations := []mutation{
			{"unknown-field", func(b map[string]any) { b["unexpected"] = true }},
			{"missing-version", func(b map[string]any) { delete(b, "version") }},
			{"null-version", func(b map[string]any) { b["version"] = nil }},
			{"unknown-schema", func(b map[string]any) { b["schema_version"] = "2.0" }},
			{"self-id-in-body", func(b map[string]any) { b["artifact_id"] = id }},
			{"non-millisecond-time", func(b map[string]any) { b["frozen_at"] = "2026-10-22T00:00:00Z" }},
			{"nested-unknown", func(b map[string]any) { b["created_by"].(map[string]any)["admin"] = true }},
		}
		switch typ {
		case canonical.TypeDomainProfile:
			mutations = append(mutations, mutation{"weakened-quorum", func(b map[string]any) {
				b["selection"].(map[string]any)["approval_votes"].(map[string]any)["R2"] = 2
			}})
		case canonical.TypePersonaSpec:
			mutations = append(mutations, mutation{"missing-investigation", func(b map[string]any) {
				b["minimum_investigation"] = []any{}
			}})
		case canonical.TypeContextManifest:
			mutations = append(mutations, mutation{"missing-persona-view", func(b map[string]any) {
				delete(b["roles"].(map[string]any), "casper")
			}}, mutation{"negative-epoch", func(b map[string]any) {
				b["context_revocation_epoch"] = -1
			}})
		}
		for _, mutation := range mutations {
			body := clone(ex.Body)
			mutation.edit(body)
			parsed, err := canonical.CanonicalizeWithLimits(encode(body), limits)
			if err != nil {
				fail("%s/%s: expected schema rejection, got C0 error: %v", ex.Name, mutation.name, err)
			}
			result := validator.Validate(schema, parsed.Bytes())
			if result == nil || result.Valid {
				fail("%s/%s: unexpectedly accepted", ex.Name, mutation.name)
			}
			negative++
		}
	}
	// Separate lexical C0 failures from JSON Schema failures.
	for _, raw := range []string{`{"version":1,"version":2}`, `{"n":1.0}`, `{"n":1e0}`, `{"n":-0}`, `{"n":9007199254740992}`} {
		if _, err := canonical.Canonicalize([]byte(raw)); err == nil {
			fail("C0 unexpectedly accepted %s", raw)
		}
		negative++
	}
	c, err := canonical.Canonicalize([]byte(`{"schema_version":"1.0"}`))
	if err != nil {
		fail("identity smoke input: %v", err)
	}
	seen := map[string]bool{}
	for _, typ := range []canonical.ArtifactType{canonical.TypeDomainProfile, canonical.TypePersonaSpec, canonical.TypeContextManifest} {
		digest, err := c.Digest(typ)
		if err != nil || seen[digest] {
			fail("type domain separation failed for %s: %v", typ, err)
		}
		seen[digest] = true
	}
	if _, err := c.Digest(canonical.ArtifactType("read_set_snapshot")); err == nil {
		fail("D-01 planned type unexpectedly registered; review the migration boundary")
	}
	if _, err := canonical.Adopt([]byte("{ \"schema_version\": \"1.0\" }")); err == nil {
		fail("storage adoption accepted non-canonical bytes")
	}
	fmt.Printf("C1-A structure: %d positive, %d negative; %d identity/adoption mutation checks; type separation and D-01 boundary OK\n", positive, negative, identity)
	fmt.Printf("D-04 policy separation: %d structural checks OK (generic budgets/windows and Persona v1.0 limits)\n", policyChecks)
	fmt.Println("Publication, semantic validators, behavior, revocation, concurrency and C1 Golden Vectors: NOT RUN")
}
