package entity

import "time"

// User is a first-class account in the harness, distinct from the static
// API keys declared in configuration. Runtime users are managed over the
// admin API and authenticated via DB-backed API keys.
type User struct {
	ID        int64
	Name      string
	Email     string
	Role      string // "admin" | "operator" | "user"
	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleUser     = "user"
)

// IsValidRole reports whether role is a recognized principal role.
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleOperator || role == RoleUser
}

// ApiKey is a DB-backed credential issued to a user. Only the SHA-256 hash
// is stored; the plaintext is shown exactly once at issuance.
type ApiKey struct {
	ID         string
	UserID     int64
	Name       string
	Prefix     string // human-readable identifier prefix, e.g. "mag_a1B2c3"
	KeyHash    string // sha256 hex
	LastUsedAt *time.Time
	Revoked    bool
	CreatedAt  time.Time
}
