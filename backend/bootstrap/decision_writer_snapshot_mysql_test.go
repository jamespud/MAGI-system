package bootstrap

import (
	"gorm.io/gorm"
	"testing"
)

func t6Snapshot(t *testing.T, db *gorm.DB) map[string][]map[string]any {
	t.Helper()
	result := map[string][]map[string]any{}
	for _, table := range []string{"decision_case", "decision_job", "decision_job_claim", "magi_agent_run", "evidence_record", "claim", "magi_vote", "reflection", "magi_tool_call", "debate_round", "magi_agent_checkpoint", "resolution", "magi_event", "magi_event_cursor"} {
		var rows []map[string]any
		if err := db.Table(table).Order("1,2").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		result[table] = rows
	}
	return result
}
