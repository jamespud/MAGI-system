// JSON Schema + typed declaration oracle for the additive JOURNAL_ADMIT v2
// contract. This is an offline check only; it issues no authority or exposure.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/validation"
)

const (
	kindString  = "string"
	kindInteger = "integer"
	kindBoolean = "boolean"
	kindArray   = "array"

	safeMax    = uint64(9007199254740991)
	idPattern  = `^[A-Za-z0-9_./:-]+$`
	digestPat  = `^[0-9a-f]{64}$`
	utcPattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}Z$`
)

type fieldShape struct {
	ref       string
	nullable  bool
	kind      string
	enum      []string
	constVal  string
	minLength *int
	maxLength *int
	pattern   string
	minimum   *uint64
	maximum   *uint64
	items     *fieldShape
	minItems  *int
	maxItems  *int
	unique    *bool
}

var (
	integerZero = fieldShape{kind: kindInteger, minimum: uintPtr(0), maximum: uintPtr(safeMax)}
	idRef       = fieldShape{ref: "V2ID"}
	digestRef   = fieldShape{ref: "V2Digest"}
	timeRef     = fieldShape{ref: "V2UTCMillis"}
	safeUIntRef = fieldShape{ref: "V2SafeUInt"}
	booleanKind = fieldShape{kind: kindBoolean}

	primitiveShapes = map[string]fieldShape{
		"V2ID":        {kind: kindString, minLength: intPtr(1), maxLength: intPtr(256), pattern: idPattern},
		"V2Digest":    {kind: kindString, pattern: digestPat},
		"V2UTCMillis": {kind: kindString, pattern: utcPattern},
		"V2SafeUInt":  integerZero,
	}

	fieldShapes = map[string]fieldShape{
		"V2Subject.subject_id":                               idRef,
		"V2Subject.identity_source_id":                       idRef,
		"V2Subject.identity_source_version":                  idRef,
		"V2ScopedEpoch.kind":                                 enumShape("AUTHORITY", "CONTEXT", "CREDENTIAL"),
		"V2ScopedEpoch.scope_id":                             idRef,
		"V2ScopedEpoch.epoch":                                safeUIntRef,
		"V2PreparedObject.id":                                idRef,
		"V2PreparedObject.digest":                            digestRef,
		"V2PreparedObject.object_ref":                        idRef,
		"V2AdmissionKey.case_id":                             idRef,
		"V2AdmissionKey.round":                               positiveUInt(),
		"V2AdmissionKey.context_manifest_id":                 idRef,
		"V2AdmissionKey.persona_id":                          enumShape("melchior", "balthasar", "casper"),
		"V2AdmissionKey.delivery_id":                         idRef,
		"V2AdmissionKey.provider_request_id":                 idRef,
		"V2HighWatermarkPlan.journal_id":                     idRef,
		"V2HighWatermarkPlan.expected":                       safeUIntRef,
		"V2HighWatermarkPlan.next":                           safeUIntRef,
		"V2ProviderReservationPlan.request_id":               idRef,
		"V2ProviderReservationPlan.session_scope_id":         idRef,
		"V2ProviderReservationPlan.expected_status":          enumShape("ABSENT"),
		"V2ProviderReservationPlan.next_status":              enumShape("RESERVED"),
		"V2PendingTransitionPlan.journal_id":                 idRef,
		"V2PendingTransitionPlan.expected":                   safeUIntRef,
		"V2PendingTransitionPlan.next":                       safeUIntRef,
		"V2ExposureFencePlan.mode":                           enumShape("AUTHORITY_ORDERED_GATE", "PROVIDER_ENFORCED_LEASE"),
		"V2ExposureFencePlan.lease_id":                       idRef,
		"V2ExposureFencePlan.lease_until":                    timeRef,
		"V2ExposureFencePlan.authority_watermark":            safeUIntRef,
		"V2ExposureFencePlan.execution_generation":           positiveUInt(),
		"V2ExposureFencePlan.scoped_epochs":                  arrayRefs("V2ScopedEpoch", 3, 3),
		"V2ExposureFencePlan.provider_capability_ref":        refShape("V2PreparedObject"),
		"V2AdmissionPlan.key":                                refShape("V2AdmissionKey"),
		"V2AdmissionPlan.principal":                          refShape("V2Subject"),
		"V2AdmissionPlan.execution_generation":               positiveUInt(),
		"V2AdmissionPlan.scoped_epochs":                      arrayRefs("V2ScopedEpoch", 3, 3),
		"V2AdmissionPlan.mutation_set":                       stringArray(1, 11, true),
		"V2AdmissionPlan.admission_input":                    refShape("V2PreparedObject"),
		"V2AdmissionPlan.journal_event_input":                refShape("V2PreparedObject"),
		"V2AdmissionPlan.dependency_edge_inputs":             arrayRefs("V2PreparedObject", 0, 64),
		"V2AdmissionPlan.high_watermark":                     refShape("V2HighWatermarkPlan"),
		"V2AdmissionPlan.provider_reservation":               refShape("V2ProviderReservationPlan"),
		"V2AdmissionPlan.pending_transition":                 refShape("V2PendingTransitionPlan"),
		"V2AdmissionPlan.fence":                              refShape("V2ExposureFencePlan"),
		"V2ExposureGatePlan.gate_id":                         idRef,
		"V2ExposureGatePlan.admission_commit_id":             idRef,
		"V2ExposureGatePlan.fence_id":                        idRef,
		"V2ExposureGatePlan.linearization_sequence":          positiveUInt(),
		"V2ExposureGatePlan.trusted_now":                     timeRef,
		"V2ProviderReceipt.receipt_id":                       idRef,
		"V2ProviderReceipt.verified":                         booleanKind,
		"V2ProviderReceipt.provider_request_id":              idRef,
		"V2ProviderReceipt.session_scope_id":                 idRef,
		"V2ProviderReceipt.applied_at":                       timeRef,
		"V2ProviderReceipt.verification_ref":                 refShape("V2PreparedObject"),
		"V2ProviderReceipt.outcome":                          enumShape("APPLIED_VERIFIED", "UNVERIFIABLE", "UNKNOWN"),
		"V2SettlementPlan.admission_commit_id":               idRef,
		"V2SettlementPlan.provider_request_id":               idRef,
		"V2SettlementPlan.expected_status":                   enumShape("ADMITTED"),
		"V2SettlementPlan.next_status":                       enumShape("APPLIED_VERIFIED", "UNVERIFIABLE"),
		"V2SettlementPlan.settlement_digest":                 digestRef,
		"V2SettlementPlan.receipt":                           refShape("V2ProviderReceipt"),
		"V2SettlementPlan.pending_transition":                refShape("V2PendingTransitionPlan"),
		"V2SettlementPlan.mutation_set":                      stringArray(1, 6, true),
		"V2AuthorityState.admission_commit_id":               stringMax(256),
		"V2AuthorityState.admission_sequence":                safeUIntRef,
		"V2AuthorityState.revocation_sequence":               safeUIntRef,
		"V2AuthorityState.revocation_scope_id":               stringMax(256),
		"V2AuthorityState.revocation_epoch":                  safeUIntRef,
		"V2AuthorityState.gate_sequence":                     safeUIntRef,
		"V2AuthorityState.settlement_sequence":               safeUIntRef,
		"V2AuthorityState.current_generation":                safeUIntRef,
		"V2AuthorityState.current_authority_epoch":           safeUIntRef,
		"V2AuthorityState.current_context_epoch":             safeUIntRef,
		"V2AuthorityState.current_credential_epoch":          safeUIntRef,
		"V2AuthorityState.trusted_now":                       timeRef,
		"V2ProviderCapability.available":                     booleanKind,
		"V2ProviderCapability.enforces_fence":                booleanKind,
		"V2ProviderCapability.supports_lease":                booleanKind,
		"V2ProviderCapability.enforces_revocation_watermark": booleanKind,
		"V2ProviderCapability.max_lease_until":               timeRef,
		"V2ExistingBinding.request_digest":                   digestRef,
		"V2ExistingBinding.admission_commit_id":              idRef,
		"V2ExistingBinding.generation":                       positiveUInt(),
		"V2ExistingBinding.status":                           enumShape("ADMITTED", "EXPOSURE_GATED", "SETTLED", "UNKNOWN", "REVOKED"),
		"V2ExistingBinding.exposure_permitted":               booleanKind,
		"V2Trace.contract_version":                           constShape("magi-authority-journal-v2/1"),
		"V2Trace.operation":                                  enumShape("JOURNAL_ADMIT_V2", "JOURNAL_EXPOSURE_GATE_V2", "JOURNAL_SETTLE_V2"),
		"V2Trace.admission":                                  refShape("V2AdmissionPlan"),
		"V2Trace.exposure_gate":                              nullableRefShape("V2ExposureGatePlan"),
		"V2Trace.settlement":                                 nullableRefShape("V2SettlementPlan"),
		"V2Trace.authority":                                  refShape("V2AuthorityState"),
		"V2Trace.provider":                                   refShape("V2ProviderCapability"),
		"V2Trace.existing_binding":                           nullableRefShape("V2ExistingBinding"),
		"V2Trace.request_digest":                             digestRef,
	}
)

func main() {
	if len(os.Args) != 2 {
		panic("expected schema path")
	}
	rawSchema, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(rawSchema, &doc); err != nil {
		panic(err)
	}
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		panic("schema has no $defs")
	}
	typed := map[string]any{
		"V2Subject":                 V2Subject{},
		"V2ScopedEpoch":             V2ScopedEpoch{},
		"V2PreparedObject":          V2PreparedObject{},
		"V2AdmissionKey":            V2AdmissionKey{},
		"V2HighWatermarkPlan":       V2HighWatermarkPlan{},
		"V2ProviderReservationPlan": V2ProviderReservationPlan{},
		"V2PendingTransitionPlan":   V2PendingTransitionPlan{},
		"V2ExposureFencePlan":       V2ExposureFencePlan{},
		"V2AdmissionPlan":           V2AdmissionPlan{},
		"V2ExposureGatePlan":        V2ExposureGatePlan{},
		"V2ProviderReceipt":         V2ProviderReceipt{},
		"V2SettlementPlan":          V2SettlementPlan{},
		"V2AuthorityState":          V2AuthorityState{},
		"V2ProviderCapability":      V2ProviderCapability{},
		"V2ExistingBinding":         V2ExistingBinding{},
	}
	for name, shape := range primitiveShapes {
		definition, ok := defs[name].(map[string]any)
		if !ok {
			panic("schema definition missing: " + name)
		}
		checkShape(name, definition, shape)
	}
	for name, value := range typed {
		definition, ok := defs[name].(map[string]any)
		if !ok {
			panic("schema definition missing: " + name)
		}
		checkObjectDefinition(name, value, definition)
	}
	checkObjectDefinition("V2Trace", V2Trace{}, doc)
	if len(defs) != len(typed)+len(primitiveShapes) {
		panic("typed/schema definition count drift")
	}

	var values []json.RawMessage
	if err = json.NewDecoder(os.Stdin).Decode(&values); err != nil {
		panic(err)
	}
	validator := validation.NewJSONSchemaValidator()
	results := make([]string, len(values))
	for i, raw := range values {
		c, err := canonical.Canonicalize(raw)
		if err != nil {
			results[i] = "C0"
			continue
		}
		result := validator.Validate(rawSchema, c.Bytes())
		if !result.Valid {
			for _, violation := range result.Violations {
				if strings.HasPrefix(violation.Code, "SCHEMA_") {
					panic(fmt.Sprintf("schema oracle invalid: %+v", result))
				}
			}
			results[i] = "SCHEMA"
		}
	}
	if err = json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}

func checkObjectDefinition(name string, value any, definition map[string]any) {
	if definition["type"] != "object" {
		panic("typed/schema type drift " + name + ": not object")
	}
	if definition["additionalProperties"] != false {
		panic("typed/schema additionalProperties drift " + name)
	}
	properties, ok := definition["properties"].(map[string]any)
	if !ok {
		panic("typed/schema properties missing " + name)
	}
	fields := jsonFieldNames(value)
	if got := sortedKeys(properties); !reflect.DeepEqual(got, fields) {
		panic(fmt.Sprintf("typed/schema property drift %s: %v != %v", name, got, fields))
	}
	checkRequiredFields(name, fields, definition["required"])
	for _, field := range fields {
		shape, ok := fieldShapes[name+"."+field]
		if !ok {
			panic("typed/schema shape missing " + name + "." + field)
		}
		checkShape(name+"."+field, properties[field], shape)
	}
}

func checkRequiredFields(name string, fields []string, raw any) {
	required, ok := raw.([]any)
	if !ok {
		panic("schema definition has no required array: " + name)
	}
	var got []string
	for _, key := range required {
		got = append(got, key.(string))
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, fields) {
		panic(fmt.Sprintf("typed/schema field drift %s: %v != %v", name, fields, got))
	}
}

func checkShape(path string, raw any, want fieldShape) {
	actual, ok := raw.(map[string]any)
	if !ok {
		panic("typed/schema shape drift " + path + ": not object")
	}
	actualWithoutDescription := make(map[string]any, len(actual))
	for key, value := range actual {
		if key != "description" {
			actualWithoutDescription[key] = value
		}
	}
	if !reflect.DeepEqual(sortedKeys(actualWithoutDescription), expectedKeys(want)) {
		panic(fmt.Sprintf("typed/schema constraint drift %s: keys %v != %v", path, sortedKeys(actualWithoutDescription), expectedKeys(want)))
	}
	if want.ref != "" {
		if want.nullable {
			oneOf, ok := actual["oneOf"].([]any)
			if !ok || len(oneOf) != 2 {
				panic("typed/schema nullable drift " + path)
			}
			first, ok := oneOf[0].(map[string]any)
			if !ok || first["$ref"] != "#/$defs/"+want.ref || len(first) != 1 {
				panic("typed/schema nullable ref drift " + path)
			}
			second, ok := oneOf[1].(map[string]any)
			if !ok || len(second) != 1 || second["type"] != "null" {
				panic("typed/schema nullable null drift " + path)
			}
			return
		}
		if actual["$ref"] != "#/$defs/"+want.ref {
			panic("typed/schema ref drift " + path)
		}
		return
	}
	switch want.kind {
	case kindString:
		if want.constVal != "" {
			if actual["const"] != want.constVal {
				panic("typed/schema const drift " + path)
			}
			return
		}
		if len(want.enum) > 0 {
			checkEnum(path, actual["enum"], want.enum)
			return
		}
		if actual["type"] != kindString {
			panic("typed/schema string type drift " + path)
		}
		checkNumberBound(path, actual, "minLength", want.minLength)
		checkNumberBound(path, actual, "maxLength", want.maxLength)
		if want.pattern != "" && actual["pattern"] != want.pattern {
			panic("typed/schema pattern drift " + path)
		}
	case kindInteger:
		if actual["type"] != kindInteger {
			panic("typed/schema integer type drift " + path)
		}
		checkUintBound(path, actual, "minimum", want.minimum)
		checkUintBound(path, actual, "maximum", want.maximum)
	case kindBoolean:
		if actual["type"] != kindBoolean {
			panic("typed/schema boolean type drift " + path)
		}
	case kindArray:
		if actual["type"] != kindArray {
			panic("typed/schema array type drift " + path)
		}
		checkNumberBound(path, actual, "minItems", want.minItems)
		checkNumberBound(path, actual, "maxItems", want.maxItems)
		if want.unique != nil {
			if actual["uniqueItems"] != *want.unique {
				panic("typed/schema uniqueItems drift " + path)
			}
		}
		if want.items == nil {
			panic("typed/schema array items missing " + path)
		}
		checkShape(path+".items", actual["items"], *want.items)
	default:
		panic("typed/schema unknown kind " + path)
	}
}

func checkEnum(path string, raw any, want []string) {
	actual, ok := raw.([]any)
	if !ok || len(actual) != len(want) {
		panic("typed/schema enum drift " + path)
	}
	for i := range want {
		if actual[i] != want[i] {
			panic("typed/schema enum drift " + path)
		}
	}
}

func checkNumberBound(path string, actual map[string]any, key string, want *int) {
	if want == nil {
		if _, ok := actual[key]; ok {
			panic("typed/schema unexpected bound " + path + "." + key)
		}
		return
	}
	if actual[key] != float64(*want) {
		panic("typed/schema bound drift " + path + "." + key)
	}
}

func checkUintBound(path string, actual map[string]any, key string, want *uint64) {
	if want == nil {
		if _, ok := actual[key]; ok {
			panic("typed/schema unexpected bound " + path + "." + key)
		}
		return
	}
	if actual[key] != float64(*want) {
		panic("typed/schema bound drift " + path + "." + key)
	}
}

func expectedKeys(shape fieldShape) []string {
	if shape.ref != "" {
		if shape.nullable {
			return []string{"oneOf"}
		}
		return []string{"$ref"}
	}
	var keys []string
	switch shape.kind {
	case kindString:
		if shape.constVal != "" {
			keys = append(keys, "const")
		} else if len(shape.enum) > 0 {
			keys = append(keys, "enum")
		} else {
			keys = append(keys, "type")
		}
		if shape.minLength != nil {
			keys = append(keys, "minLength")
		}
		if shape.maxLength != nil {
			keys = append(keys, "maxLength")
		}
		if shape.pattern != "" {
			keys = append(keys, "pattern")
		}
	case kindInteger:
		keys = append(keys, "type")
		if shape.minimum != nil {
			keys = append(keys, "minimum")
		}
		if shape.maximum != nil {
			keys = append(keys, "maximum")
		}
	case kindBoolean:
		keys = append(keys, "type")
	case kindArray:
		keys = append(keys, "type", "items")
		if shape.minItems != nil {
			keys = append(keys, "minItems")
		}
		if shape.maxItems != nil {
			keys = append(keys, "maxItems")
		}
		if shape.unique != nil {
			keys = append(keys, "uniqueItems")
		}
	}
	sort.Strings(keys)
	return keys
}

func jsonFieldNames(value any) []string {
	t := reflect.TypeOf(value)
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		fields = append(fields, t.Field(i).Tag.Get("json"))
	}
	sort.Strings(fields)
	return fields
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func refShape(ref string) fieldShape         { return fieldShape{ref: ref} }
func nullableRefShape(ref string) fieldShape { return fieldShape{ref: ref, nullable: true} }
func enumShape(values ...string) fieldShape  { return fieldShape{kind: kindString, enum: values} }
func constShape(value string) fieldShape     { return fieldShape{kind: kindString, constVal: value} }
func stringMax(max int) fieldShape           { return fieldShape{kind: kindString, maxLength: intPtr(max)} }
func positiveUInt() fieldShape {
	return fieldShape{kind: kindInteger, minimum: uintPtr(1), maximum: uintPtr(safeMax)}
}
func arrayRefs(ref string, minItems, maxItems int) fieldShape {
	return fieldShape{kind: kindArray, items: &fieldShape{ref: ref}, minItems: intPtr(minItems), maxItems: intPtr(maxItems)}
}
func stringArray(minItems, maxItems int, unique bool) fieldShape {
	return fieldShape{kind: kindArray, items: &fieldShape{kind: kindString}, minItems: intPtr(minItems), maxItems: intPtr(maxItems), unique: boolPtr(unique)}
}
func intPtr(value int) *int        { return &value }
func uintPtr(value uint64) *uint64 { return &value }
func boolPtr(value bool) *bool     { return &value }
