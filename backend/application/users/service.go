// Package users manages harness accounts and DB-backed API keys: user CRUD,
// key issuance/rotation/revocation, and self-service key listing.
package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

// ErrForbidden is returned when a non-admin calls an admin operation.
var ErrForbidden = errors.New("forbidden")

// ErrNotFound is returned when a user or key does not exist.
var ErrNotFound = errors.New("not found")

// ErrEmailTaken is returned when an account already uses the email. The column
// is not unique yet (legacy duplicates may exist), so the guard lives here.
var ErrEmailTaken = errors.New("users: email already registered")

// IssuedKey carries a freshly issued API key. Plaintext is shown exactly once.
type IssuedKey struct {
	ID        string
	Prefix    string
	Plaintext string
}

// Service is the application-layer service for users and API keys.
type Service struct {
	users            port.UserRepository
	keys             port.ApiKeyRepository
	selfRegistration bool
	invalidator      SessionInvalidator
}

// SessionInvalidator evicts cached session authorization state. Implemented by
// auth.SessionAuthorizer; the users service depends on this narrow interface
// rather than the auth package.
type SessionInvalidator interface {
	Invalidate(userID int64)
}

// UserPatch describes an admin account update. Nil fields are left unchanged.
// Only Role and Status alterations invalidate existing sessions (they bump
// auth_version); profile fields deliberately do not.
type UserPatch struct {
	Name   *string
	Email  *string
	Role   *string
	Status *string
}

// NewService creates a UsersService.
func NewService(users port.UserRepository, keys port.ApiKeyRepository) *Service {
	return &Service{users: users, keys: keys}
}

// WithSelfRegistration enables public self-registration (used by the
// auth.self_registration flag).
func WithSelfRegistration(enabled bool) func(*Service) {
	return func(s *Service) { s.selfRegistration = enabled }
}

// WithSessionInvalidator wires session-cache eviction so permission changes
// take effect immediately instead of after the cache TTL.
func WithSessionInvalidator(inv SessionInvalidator) func(*Service) {
	return func(s *Service) { s.invalidator = inv }
}

func NewServiceWithOptions(users port.UserRepository, keys port.ApiKeyRepository, opts ...func(*Service)) *Service {
	s := NewService(users, keys)
	for _, o := range opts {
		o(s)
	}
	return s
}

// CreateUser creates an account and issues its bootstrap key. The returned
// key plaintext must be shown to the caller exactly once.
func (s *Service) CreateUser(ctx context.Context, actorRole, name, role string) (*entity.User, *IssuedKey, error) {
	return s.CreateUserWithEmail(ctx, actorRole, name, "", role)
}

// ensureEmailFree rejects an email an existing account already uses. A lookup
// failure is surfaced rather than treated as "free": minting a second account
// for an email we could not check is the ambiguity this guards against.
func (s *Service) ensureEmailFree(ctx context.Context, email string) error {
	if email == "" {
		return nil
	}
	switch existing, err := s.users.FindByEmail(ctx, email); {
	case err == nil && existing != nil:
		return ErrEmailTaken
	case err == nil, errors.Is(err, port.ErrUserNotFound):
		return nil
	default:
		return fmt.Errorf("users: lookup email: %w", err)
	}
}

// CreateUserWithEmail creates an account (optionally with an identity email
// used by OIDC matching) and issues its bootstrap key.
func (s *Service) CreateUserWithEmail(ctx context.Context, actorRole, name, email, role string) (*entity.User, *IssuedKey, error) {
	if !isAdmin(actorRole) {
		return nil, nil, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("users: name is required")
	}
	if role == "" {
		role = entity.RoleUser
	}
	if !entity.IsValidRole(role) {
		return nil, nil, fmt.Errorf("users: role must be one of %q, %q, %q", entity.RoleAdmin, entity.RoleOperator, entity.RoleUser)
	}
	email = strings.TrimSpace(email)
	if utf8.RuneCountInString(name) > validation.MaxUserNameRune {
		return nil, nil, fmt.Errorf("users: name exceeds %d characters", validation.MaxUserNameRune)
	}
	if utf8.RuneCountInString(email) > validation.MaxUserEmailRune {
		return nil, nil, fmt.Errorf("users: email exceeds %d characters", validation.MaxUserEmailRune)
	}
	if err := s.ensureEmailFree(ctx, email); err != nil {
		return nil, nil, err
	}
	u := &entity.User{Name: name, Email: email, Role: role}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, nil, fmt.Errorf("users: create user: %w", err)
	}
	key, err := s.issueKey(ctx, u.ID, "bootstrap")
	if err != nil {
		// The user row exists even if key issuance fails; surface the key error.
		return u, nil, fmt.Errorf("users: issue bootstrap key: %w", err)
	}
	return u, key, nil
}

// SelfRegister creates a user with the user role and issues a bootstrap key.
// It is only allowed when self-registration is enabled.
func (s *Service) SelfRegister(ctx context.Context, name, email string) (*entity.User, *IssuedKey, error) {
	if !s.selfRegistration {
		return nil, nil, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("users: name is required")
	}
	email = strings.TrimSpace(email)
	if utf8.RuneCountInString(name) > validation.MaxUserNameRune {
		return nil, nil, fmt.Errorf("users: name exceeds %d characters", validation.MaxUserNameRune)
	}
	if utf8.RuneCountInString(email) > validation.MaxUserEmailRune {
		return nil, nil, fmt.Errorf("users: email exceeds %d characters", validation.MaxUserEmailRune)
	}
	if err := s.ensureEmailFree(ctx, email); err != nil {
		return nil, nil, err
	}
	u := &entity.User{Name: name, Email: email, Role: entity.RoleUser}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, nil, fmt.Errorf("users: create user: %w", err)
	}
	key, err := s.issueKey(ctx, u.ID, "bootstrap")
	if err != nil {
		return u, nil, fmt.Errorf("users: issue bootstrap key: %w", err)
	}
	return u, key, nil
}

// IssueKey issues a new API key for a user (admin-only, or self-service when
// the actor is the target user).
func (s *Service) IssueKey(ctx context.Context, actorID int64, actorRole string, userID int64, name string) (*IssuedKey, error) {
	if !isAdmin(actorRole) && actorID != userID {
		return nil, ErrForbidden
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, ErrNotFound
	}
	return s.issueKey(ctx, userID, strings.TrimSpace(name))
}

func (s *Service) issueKey(ctx context.Context, userID int64, name string) (*IssuedKey, error) {
	plaintext, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("users: generate key: %w", err)
	}
	k := &entity.ApiKey{
		ID:      fmt.Sprintf("ak-%s", uuid.NewString()),
		UserID:  userID,
		Name:    name,
		Prefix:  prefix,
		KeyHash: hash,
	}
	if err := s.keys.Create(ctx, k); err != nil {
		return nil, fmt.Errorf("users: persist key: %w", err)
	}
	return &IssuedKey{ID: k.ID, Prefix: prefix, Plaintext: plaintext}, nil
}

// ListUsers returns all users with their active key counts (admin-only).
func (s *Service) ListUsers(ctx context.Context, actorRole string) ([]*UserSummary, error) {
	if !isAdmin(actorRole) {
		return nil, ErrForbidden
	}
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*UserSummary, 0, len(users))
	for _, u := range users {
		keys, kerr := s.keys.ListByUser(ctx, u.ID)
		active := 0
		if kerr == nil {
			for _, k := range keys {
				if !k.Revoked {
					active++
				}
			}
		}
		out = append(out, &UserSummary{User: u, ActiveKeys: active})
	}
	return out, nil
}

// ListKeys returns a user's keys, hiding hashes (admin or self-service).
func (s *Service) ListKeys(ctx context.Context, actorID int64, actorRole string, userID int64) ([]*entity.ApiKey, error) {
	if !isAdmin(actorRole) && actorID != userID {
		return nil, ErrForbidden
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, ErrNotFound
	}
	return s.keys.ListByUser(ctx, userID)
}

// Me returns the calling user plus their own keys for self-service display.
func (s *Service) Me(ctx context.Context, userID int64) (*entity.User, []*entity.ApiKey, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	keys, err := s.keys.ListByUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return u, keys, nil
}

// RevokeKey disables a key without deleting it (admin or key owner).
func (s *Service) RevokeKey(ctx context.Context, actorID int64, actorRole string, keyID string) error {
	key, err := s.keys.GetByID(ctx, keyID)
	if err != nil {
		return ErrNotFound
	}
	if !isAdmin(actorRole) && actorID != key.UserID {
		return ErrForbidden
	}
	key.Revoked = true
	return s.keys.Update(ctx, key)
}

// RotateKey revokes a key and issues a replacement for the same user.
func (s *Service) RotateKey(ctx context.Context, actorID int64, actorRole string, keyID string) (*IssuedKey, error) {
	key, err := s.keys.GetByID(ctx, keyID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !isAdmin(actorRole) && actorID != key.UserID {
		return nil, ErrForbidden
	}
	key.Revoked = true
	if err := s.keys.Update(ctx, key); err != nil {
		return nil, err
	}
	return s.issueKey(ctx, key.UserID, key.Name+" (rotated)")
}

// DeleteUser removes a user and all their keys (admin-only).
func (s *Service) DeleteUser(ctx context.Context, actorRole string, userID int64) error {
	if !isAdmin(actorRole) {
		return ErrForbidden
	}
	keys, err := s.keys.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		_ = s.keys.Delete(ctx, k.ID)
	}
	if err := s.users.Delete(ctx, userID); err != nil {
		return err
	}
	// A cached auth state would otherwise keep the removed account valid until
	// the TTL expired.
	s.invalidate(userID)
	return nil
}

// UpdateUser applies an admin account patch. The whole patch is validated
// before anything is written and is then applied in a single UPDATE, so an
// invalid field can never leave a partially applied change behind. A role or
// status change bumps auth_version (existing cookies stop being honored);
// profile edits leave sessions intact but still evict the cache so the display
// name refreshes immediately.
func (s *Service) UpdateUser(ctx context.Context, actorRole string, userID int64, patch UserPatch) (*entity.User, error) {
	if !isAdmin(actorRole) {
		return nil, ErrForbidden
	}
	current, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	writer, ok := s.users.(port.AuthUserWriter)
	if !ok {
		return nil, fmt.Errorf("users: repository does not support versioned updates")
	}

	// Validate and normalize every field first: no writes happen until the
	// whole patch is known-good.
	var m port.UserMutation
	authorizationChanged := false
	if patch.Role != nil {
		role := strings.TrimSpace(*patch.Role)
		if !entity.IsValidRole(role) {
			return nil, fmt.Errorf("users: role must be one of %q, %q, %q", entity.RoleAdmin, entity.RoleOperator, entity.RoleUser)
		}
		if role != current.Role {
			m.Role = &role
			authorizationChanged = true
		}
	}
	if patch.Status != nil {
		status := strings.TrimSpace(*patch.Status)
		if !entity.IsValidUserStatus(status) {
			return nil, fmt.Errorf("users: status must be %q or %q", entity.UserStatusActive, entity.UserStatusDisabled)
		}
		if status != current.Status {
			m.Status = &status
			authorizationChanged = true
		}
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return nil, fmt.Errorf("users: name is required")
		}
		m.Name = &name
	}
	if patch.Email != nil {
		email := strings.TrimSpace(*patch.Email)
		m.Email = &email
	}
	m.BumpAuthVersion = authorizationChanged

	updated, err := writer.ApplyUserMutation(ctx, userID, m)
	if err != nil {
		if errors.Is(err, port.ErrUserNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Evict only after a successful write. A profile-only patch evicts too (so
	// a renamed user is not shown with the old name for a TTL) but does not
	// bump auth_version, so no session is logged out.
	s.invalidate(userID)
	return updated, nil
}

// RevokeSessions invalidates every existing session for a user by bumping
// auth_version (admin-only).
func (s *Service) RevokeSessions(ctx context.Context, actorRole string, userID int64) error {
	if !isAdmin(actorRole) {
		return ErrForbidden
	}
	writer, ok := s.users.(port.AuthUserWriter)
	if !ok {
		return fmt.Errorf("users: repository does not support versioned updates")
	}
	if _, err := writer.BumpAuthVersion(ctx, userID); err != nil {
		if errors.Is(err, port.ErrUserNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.invalidate(userID)
	return nil
}

// loadUser maps a missing account to ErrNotFound without hiding storage
// failures.
func (s *Service) loadUser(ctx context.Context, userID int64) (*entity.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, port.ErrUserNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func (s *Service) invalidate(userID int64) {
	if s.invalidator != nil {
		s.invalidator.Invalidate(userID)
	}
}

func isAdmin(role string) bool { return role == entity.RoleAdmin }

// UserSummary is a user plus derived key counts for admin listing.
type UserSummary struct {
	*entity.User
	ActiveKeys int
}
