package entity

import "time"

// RecurringCase is a user-owned decision template that the harness runs
// automatically at a minimum interval, producing a fresh DecisionCase each
// time (proactive monitoring).
type RecurringCase struct {
	ID          string
	UserID      int64
	Name        string
	Question    string
	Background  string
	Constraints []Constraint
	Interval    time.Duration
	Enabled     bool
	LastRunAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Due reports whether another run should fire at now.
func (r *RecurringCase) Due(now time.Time) bool {
	if !r.Enabled {
		return false
	}
	if r.LastRunAt == nil {
		return true
	}
	return now.Sub(*r.LastRunAt) >= r.Interval
}
