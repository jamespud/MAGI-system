package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

// The dissent tri-state must survive a persistence round trip: NULL (legacy,
// not recorded), an explicit non-nil empty slice (recorded none) and a
// non-empty slice (recorded minority) are three distinct facts.
func TestResolutionRepo_DissentTriStateRoundTrip(t *testing.T) {
	db := openConversationDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	// Simulate a legacy row written before the dissent column existed. The
	// column is nullable, so an unset value is SQL NULL.
	legacy := &entity.Resolution{ID: "res-legacy", CaseID: "c-legacy", FinalDecision: entity.VoteDecisionApprove, FinalReport: "# legacy"}
	if err := repo.ResolutionRepo().Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if err := db.Exec("UPDATE resolution SET dissent_json = NULL WHERE case_id = ?", "c-legacy").Error; err != nil {
		t.Fatalf("null out dissent: %v", err)
	}

	none := &entity.Resolution{ID: "res-none", CaseID: "c-none", FinalDecision: entity.VoteDecisionApprove, FinalReport: "# none", Dissent: []entity.Dissent{}}
	if err := repo.ResolutionRepo().Create(ctx, none); err != nil {
		t.Fatalf("create none: %v", err)
	}
	present := &entity.Resolution{ID: "res-present", CaseID: "c-present", FinalDecision: entity.VoteDecisionApprove, FinalReport: "# present",
		Dissent: []entity.Dissent{{AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject, Reasoning: "risk",
			Conditions: []entity.DecisionCondition{{Statement: "rollback", MustHold: true}}}}}
	if err := repo.ResolutionRepo().Create(ctx, present); err != nil {
		t.Fatalf("create present: %v", err)
	}

	for caseID, want := range map[string]entity.DissentState{
		"c-legacy": entity.DissentUnknown, "c-none": entity.DissentNone, "c-present": entity.DissentPresent,
	} {
		got, err := repo.ResolutionRepo().Get(ctx, caseID)
		if err != nil || got == nil {
			t.Fatalf("get %s: %+v err=%v", caseID, got, err)
		}
		if got.DissentState() != want {
			t.Fatalf("%s state = %s, want %s (dissent=%+v)", caseID, got.DissentState(), want, got.Dissent)
		}
	}
	got, _ := repo.ResolutionRepo().Get(ctx, "c-present")
	if len(got.Dissent) != 1 || got.Dissent[0].Reasoning != "risk" || len(got.Dissent[0].Conditions) != 1 {
		t.Fatalf("present dissent content lost: %+v", got.Dissent)
	}
}

// A non-NULL dissent_json that is not a well-formed JSON array is corruption,
// not "not recorded". It must fail closed instead of being downgraded to
// UNKNOWN, otherwise a stored minority record would silently vanish from the
// HTTP/A2A/export projections.
func TestResolutionRepo_CorruptDissentRecordFailsClosed(t *testing.T) {
	db := openConversationDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	res := &entity.Resolution{ID: "res-corrupt", CaseID: "c-corrupt", FinalDecision: entity.VoteDecisionApprove, FinalReport: "# r",
		Dissent: []entity.Dissent{{AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject, Reasoning: "risk"}}}
	if err := repo.ResolutionRepo().Create(ctx, res); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, bad := range []string{"{not json", "null", "", `{"agent_code":"casper"}`} {
		if err := db.Exec("UPDATE resolution SET dissent_json = ? WHERE case_id = ?", bad, "c-corrupt").Error; err != nil {
			t.Fatalf("corrupt row: %v", err)
		}
		got, err := repo.ResolutionRepo().Get(ctx, "c-corrupt")
		if err == nil {
			t.Fatalf("dissent_json=%q must fail closed, got %+v (state=%s)", bad, got, got.DissentState())
		}
		if got != nil {
			t.Fatalf("dissent_json=%q must not return a resolution, got %+v", bad, got)
		}
	}
}

// A well-formed JSON array can still contain elements that are not valid
// dissent records. Those must fail closed instead of being reported as PRESENT.
func TestResolutionRepo_InvalidDissentElementsFailClosed(t *testing.T) {
	db := openConversationDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	res := &entity.Resolution{ID: "res-elem", CaseID: "c-elem", FinalDecision: entity.VoteDecisionApprove, FinalReport: "# r",
		Dissent: []entity.Dissent{{AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject, Reasoning: "risk"}}}
	if err := repo.ResolutionRepo().Create(ctx, res); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, bad := range []string{
		`[null]`,
		`[{}]`,
		`[{"agent_code":"nobody","decision":"reject"}]`,
		`[{"agent_code":"casper","decision":"maybe"}]`,
		`[{"decision":"reject"}]`,
		`[{"agent_code":"casper"}]`,
	} {
		if err := db.Exec("UPDATE resolution SET dissent_json = ? WHERE case_id = ?", bad, "c-elem").Error; err != nil {
			t.Fatalf("corrupt row: %v", err)
		}
		got, err := repo.ResolutionRepo().Get(ctx, "c-elem")
		if err == nil {
			t.Fatalf("dissent_json=%s must fail closed, got %+v (state=%s)", bad, got, got.DissentState())
		}
	}
}
