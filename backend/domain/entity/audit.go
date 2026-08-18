package entity

import "time"

// AuditEvent is one recorded administrative/security action in the audit trail.
type AuditEvent struct {
	ID        int64
	UserID    int64
	Username  string
	Role      string
	Action    string
	Resource  string
	Detail    string
	Status    int
	CreatedAt time.Time
}
