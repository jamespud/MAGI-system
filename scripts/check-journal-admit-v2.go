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
	for name, value := range typed {
		definition, ok := defs[name].(map[string]any)
		if !ok {
			panic("schema definition missing: " + name)
		}
		checkRequiredFields(name, value, definition["required"])
	}
	checkRequiredFields("V2Trace", V2Trace{}, doc["required"])

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

func checkRequiredFields(name string, value any, raw any) {
	var fields []string
	t := reflect.TypeOf(value)
	for i := 0; i < t.NumField(); i++ {
		fields = append(fields, t.Field(i).Tag.Get("json"))
	}
	var required []string
	rawRequired, ok := raw.([]any)
	if !ok {
		panic("schema definition has no required array: " + name)
	}
	for _, key := range rawRequired {
		required = append(required, key.(string))
	}
	sort.Strings(fields)
	sort.Strings(required)
	if !reflect.DeepEqual(fields, required) {
		panic(fmt.Sprintf("typed/schema field drift %s: %v != %v", name, fields, required))
	}
}
