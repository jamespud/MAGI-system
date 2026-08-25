package assistant_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestBuildFollowUpBackground_BoundsToLatestTwentyAndSortsDeterministically(t *testing.T) {
	base := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	messages := make([]*entity.ConversationMessage, 0, 22)
	for i := 0; i < 22; i++ {
		messages = append(messages, &entity.ConversationMessage{
			ID: "msg-" + string(rune('a'+i)), Role: entity.ConversationRoleUser,
			Content: "question-" + string(rune('a'+i)), CreatedAt: base.Add(time.Duration(21-i) * time.Minute),
		})
	}
	got := assistant.BuildFollowUpBackground(messages, nil, "extra")
	if strings.Contains(got, "question-v") || strings.Contains(got, "question-u") {
		t.Fatalf("oldest messages were not bounded out: %q", got)
	}
	if !strings.Contains(got, "question-a") || !strings.Contains(got, "question-t") {
		t.Fatalf("latest messages missing: %q", got)
	}
	if strings.Index(got, "question-t") > strings.Index(got, "question-a") {
		t.Fatalf("history was not chronological: %q", got)
	}
	if !strings.Contains(got, "[Additional background]\nextra") {
		t.Fatalf("extra background missing: %q", got)
	}
}

func TestBuildFollowUpBackground_FormatsLinkedResolution(t *testing.T) {
	messages := []*entity.ConversationMessage{
		{ID: "a", Role: entity.ConversationRoleAssistant, CaseID: "case-1", CreatedAt: time.Unix(1, 0)},
	}
	resolutions := map[string]*entity.Resolution{"case-1": {
		CaseID: "case-1", FinalDecision: entity.VoteDecisionApprove,
		Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 2},
	}}
	got := assistant.BuildFollowUpBackground(messages, resolutions, "")
	if !strings.Contains(got, "Previous decision (case-1): approve (consensus=strong_approval, round=2)") {
		t.Fatalf("resolution format = %q", got)
	}
}
