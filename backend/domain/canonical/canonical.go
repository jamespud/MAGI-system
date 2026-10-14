// Package canonical implements the MAGI canonical encoding profile `magi-cj/1`
// and the content-addressed identity of MAGI authoritative artifacts.
//
// It is a strict subset of RFC 8785 (JCS) plus MAGI-specific input rules; see
// docs/canonical-encoding-contract.md. It deliberately does NOT reuse the legacy
// digest helpers in domain/execution and application/a2a, whose bytes are frozen
// for backward compatibility (`legacy-manifest/v1`, `legacy-approval-intent/v1`,
// `legacy-a2a-request/v1`).
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
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
}

// Schema-valid identifier for an artifact type: lowercase, digits and underscore.
var typeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidType reports whether t is in the closed artifact-type whitelist.
func ValidType(t ArtifactType) bool {
	_, ok := typePrefixes[t]
	return ok
}

// Canonicalize parses raw JSON strictly and returns its `magi-cj/1` canonical
// bytes. Duplicate keys, floats, negative zero, out-of-range integers, non-NFC
// strings, lone surrogates and invalid UTF-8 are rejected with an *Error.
func Canonicalize(raw []byte) ([]byte, error) {
	if err := prevalidate(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := decodeValue(dec, "$")
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errf(KindInvalidJSON, "$", "trailing data after the JSON value")
	}
	var buf bytes.Buffer
	if err := emit(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CanonicalizeStruct canonicalizes the JSON form of an in-memory value, so there
// is exactly one canonicalization path for artifacts (marshal, then Canonicalize).
func CanonicalizeStruct(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, errf(KindUnsupportedValue, "$", "marshal failed: %v", err)
	}
	return Canonicalize(raw)
}

// Digest returns the domain-separated sha256 of already-canonical bytes.
func Digest(t ArtifactType, canonicalBytes []byte) (string, error) {
	if !ValidType(t) {
		return "", errf(KindUnknownType, "$", "artifact type %q is not in the whitelist", string(t))
	}
	h := sha256.New()
	h.Write([]byte("magi:canonical:v1:" + string(t) + "\n"))
	h.Write(canonicalBytes)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ArtifactID derives the artifact identifier from the full digest. Callers can
// never supply an id: it is always <type_prefix>_<digest>.
func ArtifactID(t ArtifactType, canonicalBytes []byte) (string, error) {
	digest, err := Digest(t, canonicalBytes)
	if err != nil {
		return "", err
	}
	return typePrefixes[t] + "_" + digest, nil
}

// --- strict parsing -------------------------------------------------------

// prevalidate enforces the byte-level rules that encoding/json would otherwise
// silently "fix": invalid UTF-8, raw control characters inside strings, malformed
// escapes and lone UTF-16 surrogates.
func prevalidate(raw []byte) error {
	if !utf8.Valid(raw) {
		return errf(KindInvalidJSON, "$", "input is not valid UTF-8")
	}
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
				continue
			}
			// Non-standard JSON number constants are a MAGI float violation, not
			// structural noise; report them as such.
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
				if cp >= 0xD800 && cp <= 0xDBFF { // high surrogate needs a low one
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

func decodeValue(dec *json.Decoder, path string) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return decodeObject(dec, path)
		case '[':
			return decodeArray(dec, path)
		default:
			return nil, errf(KindInvalidJSON, path, "unexpected delimiter %q", t)
		}
	case string:
		if err := checkNFC(t, path); err != nil {
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

func decodeObject(dec *json.Decoder, path string) (any, error) {
	obj := map[string]any{}
	seen := map[string]struct{}{}
	for dec.More() {
		keyTok, err := dec.Token()
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
		if err := checkNFC(key, path); err != nil {
			return nil, err
		}
		val, err := decodeValue(dec, path+"."+key)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	return obj, nil
}

func decodeArray(dec *json.Decoder, path string) (any, error) {
	arr := []any{}
	idx := 0
	for dec.More() {
		val, err := decodeValue(dec, fmt.Sprintf("%s[%d]", path, idx))
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
		idx++
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return nil, errf(KindInvalidJSON, path, "%v", err)
	}
	return arr, nil
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
// are UTF-8, so we must translate before comparing (a byte-wise compare would
// order non-ASCII keys differently).
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
