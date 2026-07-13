package service

import (
	"context"
	"sort"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Replay reads all MagiEvents for a case from the EventStore, sorted by Timestamp.
// Used for trace/audit/replay (ADR-008).
func Replay(ctx context.Context, caseID string, repo port.EventRepository) ([]*entity.MagiEvent, error) {
	events, err := repo.ListByCase(ctx, caseID)
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events, nil
}
