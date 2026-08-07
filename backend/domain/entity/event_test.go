package entity_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestNewEvent_SetsIDTimestampPayload(t *testing.T) {
	before := time.Now()
	ac := entity.MagiCode("melchior")
	ev := entity.NewEvent("case-1", "run-1", &ac, entity.EventVoteSubmitted, map[string]any{"stance": "approve"})
	if !strings.HasPrefix(ev.ID, "case-1-") {
		t.Fatalf("ID should start with caseID, got %s", ev.ID)
	}
	if ev.Timestamp.Before(before) {
		t.Fatal("timestamp not set")
	}
	if ev.Type != entity.EventVoteSubmitted {
		t.Fatalf("type: %s", ev.Type)
	}
	if ev.AgentCode == nil || string(*ev.AgentCode) != "melchior" {
		t.Fatalf("agent code: %+v", ev.AgentCode)
	}
	if ev.RunID != "run-1" {
		t.Fatalf("run id: %s", ev.RunID)
	}
	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("payload not valid json: %v", err)
	}
	if p["stance"] != "approve" {
		t.Fatalf("payload stance: %v", p["stance"])
	}
}

func TestNewEvent_NilPayloadAndNilAgentCode(t *testing.T) {
	ev := entity.NewEvent("case-1", "", nil, entity.EventCaseCreated, nil)
	if ev.Payload != nil {
		t.Fatalf("nil payload should stay nil, got %s", string(ev.Payload))
	}
	if ev.AgentCode != nil {
		t.Fatal("nil agent code should stay nil")
	}
	if ev.ID == "" {
		t.Fatal("ID should still be set")
	}
}

func TestNewEvent_IDsAreUnique(t *testing.T) {
	a := entity.NewEvent("c1", "", nil, entity.EventCaseCreated, nil)
	b := entity.NewEvent("c1", "", nil, entity.EventCaseCreated, nil)
	if a.ID == b.ID {
		t.Fatal("two events should have different IDs")
	}
}

func TestNewEvent_IDsAreUniqueConcurrently(t *testing.T) {
	const count = 1000
	ids := make(chan string, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			ids <- entity.NewEvent("c1", "", nil, entity.EventCaseCreated, nil).ID
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, count)
	for id := range ids {
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate event ID: %s", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("got %d unique IDs, want %d", len(seen), count)
	}
}
