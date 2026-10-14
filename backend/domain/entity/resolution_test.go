package entity_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestValidateDissent(t *testing.T) {
	valid := []entity.Dissent{
		{AgentCode: entity.MagiCodeMelchior, Decision: entity.VoteDecisionReject},
		{AgentCode: entity.MagiCodeBalthasar, Decision: entity.VoteDecisionApprove},
		{AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionConditionalApprove},
	}
	for i, d := range valid {
		if err := entity.ValidateDissent(d); err != nil {
			t.Fatalf("valid[%d] rejected: %v", i, err)
		}
	}
	invalid := []entity.Dissent{
		{},                                    // zero value from `[null]` / `[{}]`
		{AgentCode: entity.MagiCodeCasper},    // no decision
		{Decision: entity.VoteDecisionReject}, // no persona
		{AgentCode: "nobody", Decision: entity.VoteDecisionReject},                 // unknown persona
		{AgentCode: entity.MagiCodeCommander, Decision: entity.VoteDecisionReject}, // commander does not vote
		{AgentCode: entity.MagiCodeCasper, Decision: "maybe"},                      // undefined decision
	}
	for i, d := range invalid {
		if err := entity.ValidateDissent(d); err == nil {
			t.Fatalf("invalid[%d] accepted: %+v", i, d)
		}
	}
}
