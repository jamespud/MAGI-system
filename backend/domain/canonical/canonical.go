// Package canonical implements the MAGI canonical encoding profile `magi-cj/1`
// and the content-addressed identity of MAGI authoritative artifacts.
//
// It is a strict subset of RFC 8785 (JCS) plus MAGI-specific input rules; see
// docs/canonical-encoding-contract.md. It deliberately does NOT reuse the legacy
// digest helpers in domain/execution and application/a2a, whose bytes are frozen
// for backward compatibility (`legacy-manifest/v1`, `legacy-approval-intent/v1`,
// `legacy-a2a-request/v1`).
//
// The identity API is safe by construction: Canonical is an opaque value that
// only this package can create, so Digest/ArtifactID can never be applied to
// arbitrary caller-supplied bytes.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Profile is the canonical encoding profile identifier recorded with artifacts.
const Profile = "magi-cj/1"

// DigestAlg is the digest algorithm recorded with artifacts.
const DigestAlg = "sha256"

// MaxSafeInteger is the largest integer exactly representable in an IEEE-754
// double, i.e. 2^53-1. Integers outside ±MaxSafeInteger are rejected.
const MaxSafeInteger = int64(1)<<53 - 1

// Error kinds. These strings are part of the contract: golden vectors assert
// them, so they must stay stable.
const (
	KindInvalidJSON       = "invalid_json"
	KindDuplicateKey      = "duplicate_key"
	KindFloatNumber       = "float_number"
	KindNegativeZero      = "negative_zero"
	KindIntegerOutOfRange = "integer_out_of_range"
	KindNotNFC            = "not_nfc"
	KindLoneSurrogate     = "lone_surrogate"
	KindUnknownType       = "unknown_type"
	KindUnsupportedValue  = "unsupported_value"
	KindInvalidUTF8       = "invalid_utf8"
	KindCustomMarshaler   = "custom_marshaler"
	KindPayloadTooLarge   = "payload_too_large"
	KindTooDeep           = "too_deep"
	KindTooManyMembers    = "too_many_members"
	KindStringTooLong     = "string_too_long"
	KindNonCanonical      = "non_canonical"
)

// Error is a canonicalization failure. It is always returned, never panicked.
type Error struct {
	Kind   string
	Path   string
	Detail string
}

func (e *Error) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s at %s: %s", e.Kind, e.Path, e.Detail)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Detail)
}

func errf(kind, path, format string, args ...any) *Error {
	return &Error{Kind: kind, Path: path, Detail: fmt.Sprintf(format, args...)}
}

// Limits bounds the resources a single parse/canonicalize call may consume.
// The defaults are C0 implementation defaults; artifact-specific ceilings are
// frozen by the owning artifact contract (C1/C2/C3) and passed explicitly via
// CanonicalizeWithLimits. Untrusted input MUST NOT be parsed without a ceiling.
type Limits struct {
	MaxBytes       int // total input size
	MaxDepth       int // nesting depth (root = 1)
	MaxMembers     int // total object keys + array elements
	MaxStringBytes int // length of any single string (value or key)
}

// DefaultLimits returns the C0 defaults (1 MiB payload, depth 32, 4096 members,
// 256 KiB per string).
func DefaultLimits() Limits {
	return Limits{MaxBytes: 1 << 20, MaxDepth: 32, MaxMembers: 4096, MaxStringBytes: 256 << 10}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxMembers <= 0 {
		l.MaxMembers = d.MaxMembers
	}
	if l.MaxStringBytes <= 0 {
		l.MaxStringBytes = d.MaxStringBytes
	}
	return l
}

// Canonical is canonical UTF-8 bytes produced by this package. The field is
// unexported so a caller cannot fabricate the precondition Digest relies on.
type Canonical struct{ b []byte }

// Bytes returns a copy of the canonical bytes.
func (c Canonical) Bytes() []byte { return append([]byte(nil), c.b...) }

// String returns the canonical bytes as a string.
func (c Canonical) String() string { return string(c.b) }

// IsZero reports whether the value was not produced by this package.
func (c Canonical) IsZero() bool { return c.b == nil }

// ArtifactType identifies the kind of authoritative artifact. The set is closed:
// unknown types are rejected, and the domain separator is built from it.
type ArtifactType string

// Artifact types and their id prefixes.
const (
	TypeDecisionCharter    ArtifactType = "decision_charter"
	TypeDomainProfile      ArtifactType = "domain_profile"
	TypePersonaSpec        ArtifactType = "persona_spec"
	TypeContextManifest    ArtifactType = "context_manifest"
	TypeCandidateSnapshot  ArtifactType = "candidate_snapshot"
	TypeSealedBallot       ArtifactType = "sealed_ballot"
	TypeBallotSet          ArtifactType = "ballot_set"
	TypeDecisionRecord     ArtifactType = "decision_record"
	TypeAuthorityProof     ArtifactType = "authority_proof"
	TypeActionIntent       ArtifactType = "action_intent"
	TypeAuditReceipt       ArtifactType = "audit_receipt"
	TypeSendRecord         ArtifactType = "send_record"
	TypeAcceptanceManifest ArtifactType = "acceptance_manifest"
	TypeReadSetSnapshot    ArtifactType = "read_set_snapshot"
)

var typePrefixes = map[ArtifactType]string{
	TypeDecisionCharter:    "chtr",
	TypeDomainProfile:      "dprf",
	TypePersonaSpec:        "pspc",
	TypeContextManifest:    "ctxm",
	TypeCandidateSnapshot:  "csnp",
	TypeSealedBallot:       "sbll",
	TypeBallotSet:          "bset",
	TypeDecisionRecord:     "drec",
	TypeAuthorityProof:     "aprf",
	TypeActionIntent:       "aint",
	TypeAuditReceipt:       "arcp",
	TypeSendRecord:         "srec",
	TypeAcceptanceManifest: "aman",
	TypeReadSetSnapshot:    "rset",
}

// ValidType reports whether t is in the closed artifact-type whitelist.
func ValidType(t ArtifactType) bool {
	_, ok := typePrefixes[t]
	return ok
}

// Canonicalize strictly parses raw JSON under DefaultLimits and returns its
// `magi-cj/1` canonical bytes. Duplicate keys, floats, negative zero,
// out-of-range integers, non-NFC strings, lone surrogates, invalid UTF-8,
// raw control bytes and oversized payloads are rejected with an *Error.
func Canonicalize(raw []byte) (Canonical, error) {
	return CanonicalizeWithLimits(raw, DefaultLimits())
}

// CanonicalizeWithLimits is Canonicalize with explicit resource ceilings.
func CanonicalizeWithLimits(raw []byte, limits Limits) (Canonical, error) {
	limits = limits.withDefaults()
	if len(raw) > limits.MaxBytes {
		return Canonical{}, errf(KindPayloadTooLarge, "$", "input is %d bytes, limit is %d", len(raw), limits.MaxBytes)
	}
	if err := prevalidate(raw); err != nil {
		return Canonical{}, err
	}
	p := &parser{dec: json.NewDecoder(bytes.NewReader(raw)), lim: limits}
	p.dec.UseNumber()
	value, err := p.value("$", 1)
	if err != nil {
		return Canonical{}, err
	}
	if _, err := p.dec.Token(); err != io.EOF {
		return Canonical{}, errf(KindInvalidJSON, "$", "trailing data after the JSON value")
	}
	var buf bytes.Buffer
	if err := emit(&buf, value); err != nil {
		return Canonical{}, err
	}
	return Canonical{b: buf.Bytes()}, nil
}

// Adopt validates that raw bytes are already canonical and returns them as a
// Canonical value. Use it for bytes read from storage: it both re-parses and
// requires byte-for-byte equality, so a non-canonical or corrupt row is an error
// rather than a silently different identity.
func Adopt(raw []byte) (Canonical, error) {
	canon, err := Canonicalize(raw)
	if err != nil {
		return Canonical{}, err
	}
	if !bytes.Equal(canon.b, raw) {
		return Canonical{}, errf(KindNonCanonical, "$", "input bytes are valid JSON but not canonical")
	}
	return canon, nil
}

// CanonicalizeStruct canonicalizes an in-memory value. The value is validated
// recursively BEFORE marshalling, because encoding/json is lossy: it renders
// float64(1.0) as `1` and replaces invalid UTF-8 with U+FFFD, which would hide an
// input the contract must reject.
func CanonicalizeStruct(v any) (Canonical, error) {
	return CanonicalizeStructWithLimits(v, DefaultLimits())
}

// CanonicalizeStructWithLimits is CanonicalizeStruct with explicit ceilings.
func CanonicalizeStructWithLimits(v any, limits Limits) (Canonical, error) {
	limits = limits.withDefaults()
	budget := &inMemoryBudget{lim: limits}
	if err := validateInMemory(reflect.ValueOf(v), "$", 1, budget); err != nil {
		return Canonical{}, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Canonical{}, errf(KindUnsupportedValue, "$", "marshal failed: %v", err)
	}
	return CanonicalizeWithLimits(raw, limits)
}

// Digest returns the domain-separated sha256 of this canonical value.
func (c Canonical) Digest(t ArtifactType) (string, error) {
	if c.b == nil {
		return "", errf(KindNonCanonical, "$", "empty Canonical value")
	}
	if !ValidType(t) {
		return "", errf(KindUnknownType, "$", "artifact type %q is not in the whitelist", string(t))
	}
	h := sha256.New()
	h.Write([]byte("magi:canonical:v1:" + string(t) + "\n"))
	h.Write(c.b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ArtifactID derives the artifact identifier from the full digest. Callers can
// never supply an id: it is always <type_prefix>_<digest>.
func (c Canonical) ArtifactID(t ArtifactType) (string, error) {
	digest, err := c.Digest(t)
	if err != nil {
		return "", err
	}
	return typePrefixes[t] + "_" + digest, nil
}

// --- in-memory validation (lossless-marshal precondition) -----------------

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*interface{ MarshalText() ([]byte, error) })(nil)).Elem()
)

// hasCustomMarshaler reports whether encoding/json could route this type through
// a user-defined marshaler instead of the struct/map we validated. It checks the
// pointer type as well: encoding/json calls a pointer-receiver MarshalJSON on
// addressable struct fields even when the field's value type does not implement
// the interface, so checking only v.Type() leaves a bypass.
func hasCustomMarshaler(t reflect.Type) bool {
	if t == nil || t.Kind() == reflect.Invalid {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Invalid {
		return false
	}
	if t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType) {
		return true
	}
	ptr := reflect.PointerTo(t)
	return ptr.Implements(jsonMarshalerType) || ptr.Implements(textMarshalerType)
}

// inMemoryBudget tracks the resource ceilings for the struct path BEFORE the
// value is handed to json.Marshal. bytes is a worst-case upper bound of the
// encoded size, so an over-budget object is rejected without serializing it.
type inMemoryBudget struct {
	lim     Limits
	members int
	bytes   int64
}

func (b *inMemoryBudget) member(path string) error {
	b.members++
	if b.members > b.lim.MaxMembers {
		return errf(KindTooManyMembers, path, "member count exceeds %d", b.lim.MaxMembers)
	}
	// One byte per member covers the comma separator. Counting it for the first
	// member too is deliberate: over-counting keeps this a strict upper bound.
	return b.add(1, path)
}

func (b *inMemoryBudget) add(n int, path string) error {
	b.bytes += int64(n)
	if b.bytes > int64(b.lim.MaxBytes) {
		return errf(KindPayloadTooLarge, path, "encoded size upper bound exceeds %d bytes", b.lim.MaxBytes)
	}
	return nil
}

// maxEncodedStringBytes is the worst-case JSON size of a string: every byte may
// escape to \u00XX.
func maxEncodedStringBytes(n int) int { return 6*n + 2 }

// jsonFieldName resolves the object key encoding/json will emit for a struct
// field. It mirrors encoding/json's tag rules for the parts that affect the key
// name and rejects `,string`, whose re-encoding can be larger than the value's
// own worst-case budget.
func jsonFieldName(f reflect.StructField) (name string, skip bool, err error) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true, nil
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	} else if !isValidJSONTagName(name) {
		// encoding/json silently falls back to the Go field name for tag names
		// that fail its character rules. Rather than mirror that fallback (and
		// risk budgeting the wrong key), treat the tag as unsupported.
		return "", false, fmt.Errorf("json tag name %q is not a valid encoding/json field name", name)
	}
	for _, opt := range parts[1:] {
		switch opt {
		case "string":
			return "", false, fmt.Errorf(`json option "string" changes the encoding shape; use an explicit string field`)
		case "", "omitempty":
			// omitempty may omit the field, so counting it keeps the budget an
			// upper bound.
		default:
			// Unknown options do not change the key name in encoding/json.
		}
	}
	return name, false, nil
}

// isValidJSONTagName mirrors encoding/json's isValidTag: letters, digits and
// the punctuation set it accepts. Anything else (e.g. an emoji) makes
// encoding/json ignore the tag and use the Go field name instead.
func isValidJSONTagName(s string) bool {
	if s == "" {
		return false
	}
	const punctuation = "!#$%&()*+-./:;<=>?@[]^_{|}~ "
	for _, r := range s {
		switch {
		case strings.ContainsRune(punctuation, r):
		case unicode.IsLetter(r), unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}

// validateInMemory rejects values whose JSON rendering would silently differ
// from the in-memory value (floats, invalid UTF-8, custom marshalers) and
// enforces the same resource ceilings as the JSON path before marshalling.
func validateInMemory(v reflect.Value, path string, depth int, b *inMemoryBudget) error {
	if depth > b.lim.MaxDepth {
		return errf(KindTooDeep, path, "value nesting exceeds %d", b.lim.MaxDepth)
	}
	if !v.IsValid() {
		return b.add(4, path)
	}
	if v.CanInterface() && hasCustomMarshaler(v.Type()) {
		return errf(KindCustomMarshaler, path, "type %s can marshal itself; encode it explicitly", v.Type())
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return b.add(4, path)
		}
		return validateInMemory(v.Elem(), path, depth, b)
	case reflect.Bool:
		return b.add(5, path)
	case reflect.String:
		s := v.String()
		if !utf8.ValidString(s) {
			return errf(KindInvalidUTF8, path, "string is not valid UTF-8")
		}
		if len(s) > b.lim.MaxStringBytes {
			return errf(KindStringTooLong, path, "string is %d bytes, limit is %d", len(s), b.lim.MaxStringBytes)
		}
		if err := b.add(maxEncodedStringBytes(len(s)), path); err != nil {
			return err
		}
		return checkNFC(s, path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		if n > MaxSafeInteger || n < -MaxSafeInteger {
			return errf(KindIntegerOutOfRange, path, "integer %d is outside ±%d", n, MaxSafeInteger)
		}
		return b.add(20, path)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.Uint() > uint64(MaxSafeInteger) {
			return errf(KindIntegerOutOfRange, path, "integer %d is outside ±%d", v.Uint(), MaxSafeInteger)
		}
		return b.add(20, path)
	case reflect.Float32, reflect.Float64:
		return errf(KindFloatNumber, path, "floating point values are not allowed in authoritative artifacts")
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return errf(KindUnsupportedValue, path, "[]byte is implicitly base64-encoded by encoding/json; use an explicit string or array")
		}
		if err := b.add(2, path); err != nil {
			return err
		}
		for i := 0; i < v.Len(); i++ {
			if err := b.member(path); err != nil {
				return err
			}
			if err := validateInMemory(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1, b); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return errf(KindUnsupportedValue, path, "map keys must be strings, got %s", v.Type().Key())
		}
		if err := b.add(2, path); err != nil {
			return err
		}
		iter := v.MapRange()
		for iter.Next() {
			if err := b.member(path); err != nil {
				return err
			}
			key := iter.Key().String()
			if !utf8.ValidString(key) {
				return errf(KindInvalidUTF8, path, "map key is not valid UTF-8")
			}
			if len(key) > b.lim.MaxStringBytes {
				return errf(KindStringTooLong, path, "map key is %d bytes, limit is %d", len(key), b.lim.MaxStringBytes)
			}
			if err := b.add(maxEncodedStringBytes(len(key))+1, path); err != nil {
				return err
			}
			if err := checkNFC(key, path); err != nil {
				return err
			}
			if err := validateInMemory(iter.Value(), path+"."+key, depth+1, b); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		if err := b.add(2, path); err != nil {
			return err
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			// Check anonymity BEFORE exportedness: encoding/json marshals the
			// exported fields of an embedded struct even when the embedded type
			// itself is unexported, so skipping it here would leave those values
			// unvalidated and unbudgeted.
			if field.Anonymous {
				return errf(KindUnsupportedValue, path+"."+field.Name, "embedded/anonymous struct fields are not supported; use a named field")
			}
			if !field.IsExported() {
				continue
			}
			name, skip, err := jsonFieldName(field)
			if err != nil {
				return errf(KindUnsupportedValue, path+"."+field.Name, "%v", err)
			}
			if skip {
				continue
			}
			if len(name) > b.lim.MaxStringBytes {
				return errf(KindStringTooLong, path+"."+name, "field name is %d bytes, limit is %d", len(name), b.lim.MaxStringBytes)
			}
			// Exported fields become object members, so they consume the same
			// member and key budgets as an explicit map entry.
			if err := b.member(path); err != nil {
				return err
			}
			if err := b.add(maxEncodedStringBytes(len(name))+1, path); err != nil {
				return err
			}
			if err := validateInMemory(v.Field(i), path+"."+name, depth+1, b); err != nil {
				return err
			}
		}
		return nil
	default:
		return errf(KindUnsupportedValue, path, "unsupported value kind %s", v.Kind())
	}
}

// --- strict parsing -------------------------------------------------------

type parser struct {
	dec     *json.Decoder
	lim     Limits
	members int
}

func (p *parser) value(path string, depth int) (any, error) {
	if depth > p.lim.MaxDepth {
		return nil, errf(KindTooDeep, path, "value nesting exceeds %d", p.lim.MaxDepth)
	}
	tok, err := p.dec.Token()
	if err != nil {
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return p.object(path, depth)
		case '[':
			return p.array(path, depth)
		default:
			return nil, errf(KindInvalidJSON, path, "unexpected delimiter %q", t)
		}
	case string:
		if err := checkStringLimits(t, path, p.lim); err != nil {
			return nil, err
		}
		return t, nil
	case json.Number:
		return canonicalInteger(t, path)
	case bool:
		return t, nil
	case nil:
		return nil, nil
	default:
		return nil, errf(KindInvalidJSON, path, "unexpected token %v", t)
	}
}

func (p *parser) countMember(path string) error {
	p.members++
	if p.members > p.lim.MaxMembers {
		return errf(KindTooManyMembers, path, "member count exceeds %d", p.lim.MaxMembers)
	}
	return nil
}

func (p *parser) object(path string, depth int) (any, error) {
	obj := map[string]any{}
	seen := map[string]struct{}{}
	for p.dec.More() {
		if err := p.countMember(path); err != nil {
			return nil, err
		}
		keyTok, err := p.dec.Token()
		if err != nil {
			return nil, errf(KindInvalidJSON, path, "%v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errf(KindInvalidJSON, path, "object key is not a string")
		}
		// encoding/json unescapes keys, so {"a":1,"\u0061":2} collides here.
		if _, dup := seen[key]; dup {
			return nil, errf(KindDuplicateKey, path, "duplicate object key %q", key)
		}
		seen[key] = struct{}{}
		if err := checkStringLimits(key, path, p.lim); err != nil {
			return nil, err
		}
		val, err := p.value(path+"."+key, depth+1)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	if _, err := p.dec.Token(); err != nil { // consume '}'
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	return obj, nil
}

func (p *parser) array(path string, depth int) (any, error) {
	arr := []any{}
	for p.dec.More() {
		if err := p.countMember(path); err != nil {
			return nil, err
		}
		val, err := p.value(fmt.Sprintf("%s[%d]", path, len(arr)), depth+1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	if _, err := p.dec.Token(); err != nil { // consume ']'
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	return arr, nil
}

// prevalidate enforces the byte-level rules that encoding/json would otherwise
// silently "fix": invalid UTF-8, raw control characters inside strings, malformed
// escapes and lone UTF-16 surrogates.
func prevalidate(raw []byte) error {
	if !utf8.Valid(raw) {
		return errf(KindInvalidUTF8, "$", "input is not valid UTF-8")
	}
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
				continue
			}
			rest := raw[i:]
			if bytes.HasPrefix(rest, []byte("NaN")) || bytes.HasPrefix(rest, []byte("Infinity")) ||
				bytes.HasPrefix(rest, []byte("-Infinity")) {
				return errf(KindFloatNumber, "$", "non-standard number constant is not allowed")
			}
			continue
		}
		switch {
		case c == '"':
			inString = false
		case c == '\\':
			if i+1 >= len(raw) {
				return errf(KindInvalidJSON, "$", "unterminated escape")
			}
			esc := raw[i+1]
			i++
			switch esc {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				if i+4 >= len(raw) {
					return errf(KindInvalidJSON, "$", "truncated \\u escape")
				}
				cp, ok := parseHex4(raw[i+1 : i+5])
				if !ok {
					return errf(KindInvalidJSON, "$", "invalid \\u escape")
				}
				i += 4
				if cp >= 0xD800 && cp <= 0xDBFF {
					if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
						return errf(KindLoneSurrogate, "$", "high surrogate without a low surrogate")
					}
					low, ok := parseHex4(raw[i+3 : i+7])
					if !ok || low < 0xDC00 || low > 0xDFFF {
						return errf(KindLoneSurrogate, "$", "high surrogate without a valid low surrogate")
					}
					i += 6
				} else if cp >= 0xDC00 && cp <= 0xDFFF {
					return errf(KindLoneSurrogate, "$", "lone low surrogate")
				}
			default:
				return errf(KindInvalidJSON, "$", "invalid escape \\%c", esc)
			}
		case c < 0x20:
			return errf(KindInvalidJSON, "$", "raw control character 0x%02x must be escaped", c)
		}
	}
	if inString {
		return errf(KindInvalidJSON, "$", "unterminated string")
	}
	return nil
}

func parseHex4(b []byte) (rune, bool) {
	if len(b) != 4 {
		return 0, false
	}
	var v rune
	for _, c := range b {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			v |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}

var integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// canonicalInteger applies the MAGI number restrictions: integer literals only,
// "-0" rejected, and the ±(2^53-1) cross-language safe range.
func canonicalInteger(n json.Number, path string) (int64, error) {
	s := n.String()
	if s == "-0" {
		return 0, errf(KindNegativeZero, path, "negative zero is not a canonical integer")
	}
	if !integerPattern.MatchString(s) {
		return 0, errf(KindFloatNumber, path, "only decimal integer literals are allowed, got %q", s)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errf(KindIntegerOutOfRange, path, "integer %q does not fit int64", s)
	}
	if v > MaxSafeInteger || v < -MaxSafeInteger {
		return 0, errf(KindIntegerOutOfRange, path, "integer %q is outside ±%d", s, MaxSafeInteger)
	}
	return v, nil
}

func checkStringLimits(s, path string, lim Limits) error {
	if len(s) > lim.MaxStringBytes {
		return errf(KindStringTooLong, path, "string is %d bytes, limit is %d", len(s), lim.MaxStringBytes)
	}
	return checkNFC(s, path)
}

// checkNFC enforces the MAGI rule that canonical text is NFC *verified*, never
// silently normalized.
func checkNFC(s, path string) error {
	if !norm.NFC.IsNormalString(s) {
		return errf(KindNotNFC, path, "string is not NFC-normalized")
	}
	return nil
}

// --- canonical emission ---------------------------------------------------

func emit(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case string:
		writeString(buf, t)
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := emit(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeString(buf, k)
			buf.WriteByte(':')
			if err := emit(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return errf(KindUnsupportedValue, "$", "unsupported value type %T", v)
	}
	return nil
}

// writeString emits RFC 8785 minimal escaping: only '"', '\\' and U+0000..U+001F
// are escaped. HTML escaping (\\u003c) is deliberately NOT applied.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\t':
			buf.WriteString(`\t`)
		case '\n':
			buf.WriteString(`\n`)
		case '\f':
			buf.WriteString(`\f`)
		case '\r':
			buf.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
				continue
			}
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}

// lessUTF16 orders strings by UTF-16 code units, as RFC 8785 requires. Go strings
// are UTF-8, so we must translate before comparing.
func lessUTF16(a, b string) bool {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// Errors is exported for errors.Is-style checks by kind.
func KindOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}
