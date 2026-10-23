package inputartifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var versionParts = regexp.MustCompile(`[^a-z0-9]+`)

func fixedVersion(version, path string) error {
	if version == "" {
		return invalid("VERSION", path, "empty reference version")
	}
	for _, p := range versionParts.Split(strings.ToLower(version), -1) {
		switch p {
		case "latest", "current", "stable", "main", "master", "head", "default", "active", "newest", "next", "x":
			return invalid("VERSION", path, "mutable or ambiguous version alias")
		}
	}
	return nil
}
func pendingReferences(d DomainProfile) ([]Dependency, error) {
	// Walk only a Schema-checked typed body. The traversal is deterministic and
	// preserves a separate path for every declaration, including repeated refs.
	b, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if e = dec.Decode(&value); e != nil {
		return nil, e
	}
	var pending []Dependency
	seenContent := map[string]string{}
	var walk func(any, string) error
	walk = func(v any, path string) error {
		switch x := v.(type) {
		case map[string]any:
			if _, ok := x["content_digest"]; ok {
				raw, e := json.Marshal(x)
				if e != nil {
					return e
				}
				var ref ExternalRef
				if e = json.Unmarshal(raw, &ref); e != nil {
					return e
				}
				if e = fixedVersion(ref.Version, path+".version"); e != nil {
					return e
				}
				refIdentity := ref.Namespace + "\x00" + ref.ObjectID + "\x00" + ref.Version
				if prior, ok := seenContent[refIdentity]; ok && prior != ref.ContentDigest {
					return invalid("REFERENCE", path+".content_digest", "same external object/version has conflicting content digests")
				}
				seenContent[refIdentity] = ref.ContentDigest
				pending = append(pending, Dependency{Kind: "UNVERIFIABLE_REFERENCE", Path: path, Reference: ref, Detail: "original bytes, immutable source/type binding, issuer/method qualification, policy compatibility, expiry and revocation not verified"})
				return nil
			}
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if e := walk(x[k], path+"."+k); e != nil {
					return e
				}
			}
		case []any:
			for i, v := range x {
				if e := walk(v, fmt.Sprintf("%s[%d]", path, i)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e = walk(value, "$"); e != nil {
		return nil, e
	}
	if e = fixedVersion(d.CreatedBy.IdentitySourceVersion, "$.created_by.identity_source_version"); e != nil {
		return nil, e
	}
	pending = append(pending, Dependency{Kind: "UNVERIFIABLE_IDENTITY", Path: "$.created_by", Detail: "publisher and identity-source assertions are unauthenticated; C2 governance root required"})
	pending = append(pending, Dependency{Kind: "UNVERIFIABLE_POLICY_BINDING", Path: "$.risk_policy", Detail: "local code types checked; external rules/method/parameter registration and governance compatibility unresolved"})
	if d.Budget.CostLimit != nil {
		pending = append(pending, Dependency{Kind: "UNVERIFIABLE_BILLING_MAPPING", Path: "$.budget.cost_limit", Detail: "currency/exponent shape only; no trusted billing or currency minor-unit mapping"})
	}
	return pending, nil
}
