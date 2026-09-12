package entity

import "time"

// User is a first-class account in the harness, distinct from the static
// API keys declared in configuration. Runtime users are managed over the
// admin API and authenticated via DB-backed API keys.
type User struct {
	ID    int64
	Name  string
	Email string
	Role  string // "admin" | "operator" | "user"
	// Status is "" (legacy rows created before the column existed) | "active" |
	// "disabled".
	Status string
	// AuthVersion is bumped whenever a change alters what an existing session is
	// allowed to do: role change, disable/re-enable, or an explicit revoke-all.
	// Profile edits (name/email) deliberately do NOT bump it. Sessions embed the
	// version they were minted with and stop being honored once it no longer
	// matches the stored value.
	AuthVersion int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleUser     = "user"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

// IsActive reports whether the account may authenticate. An empty status is a
// row created before the column existed and is treated as active.
func (u *User) IsActive() bool {
	return u.Status == "" || u.Status == UserStatusActive
}

// IsValidUserStatus reports whether status is a recognized account status.
func IsValidUserStatus(status string) bool {
	return status == UserStatusActive || status == UserStatusDisabled
}

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
