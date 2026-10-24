// Existing JSON Schema engine + typed API parity oracle for synthetic traces.
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
	schema, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(schema, &doc); err != nil {
		panic(err)
	}
	defs := doc["$defs"].(map[string]any)
	for name, value := range map[string]any{"Subject": Subject{}, "CaseRound": CaseRound{}, "ScopedEpoch": ScopedEpoch{}, "Dependency": ExactDependency{}, "VerifyRequest": VerifyRequest{}, "VerifiedToken": VerifiedToken{}, "ExpectedState": ExpectedState{}, "IntentBinding": IntentBinding{}, "PreparedObject": PreparedObject{}, "HighWatermarkPlan": HighWatermarkPlan{}, "ProviderSettlementPlan": ProviderSettlementPlan{}, "PendingTransitionPlan": PendingTransitionPlan{}, "JournalAdmissionPlan": JournalAdmissionPlan{}, "StateTransitionPlan": StateTransitionPlan{}, "QuiescencePlan": QuiescencePlan{}, "JournalClosurePlan": JournalClosurePlan{}, "CandidateClosurePlan": CandidateClosurePlan{}, "ActionTransitionPlan": ActionTransitionPlan{}, "QuotaTransitionPlan": QuotaTransitionPlan{}, "LogicalOperationPlan": LogicalOperationPlan{}, "SendPlan": SendPlan{}, "CompleteWritePlan": CompleteWritePlan{}} {
		var fields []string
		t := reflect.TypeOf(value)
		for i := 0; i < t.NumField(); i++ {
			fields = append(fields, t.Field(i).Tag.Get("json"))
		}
		var required []string
		for _, key := range defs[name].(map[string]any)["required"].([]any) {
			required = append(required, key.(string))
		}
		sort.Strings(fields)
		sort.Strings(required)
		if !reflect.DeepEqual(fields, required) {
			panic(fmt.Sprintf("typed/schema field drift %s: %v != %v", name, fields, required))
		}
	}
	var values []json.RawMessage
	if err = json.NewDecoder(os.Stdin).Decode(&values); err != nil {
		panic(err)
	}
	validator := validation.NewJSONSchemaValidator()
	results := make([]string, len(values))
	for i, raw := range values {
		c, e := canonical.Canonicalize(raw)
		if e != nil {
			results[i] = "C0"
			continue
		}
		result := validator.Validate(schema, c.Bytes())
		if !result.Valid {
			for _, v := range result.Violations {
				if strings.HasPrefix(v.Code, "SCHEMA_") {
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
