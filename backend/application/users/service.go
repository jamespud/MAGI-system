// Package users manages harness accounts and DB-backed API keys: user CRUD,
// key issuance/rotation/revocation, and self-service key listing.
package users

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ErrForbidden is returned when a non-admin calls an admin operation.
var ErrForbidden = errors.New("forbidden")

// ErrNotFound is returned when a user or key does not exist.
var ErrNotFound = errors.New("not found")

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
	u := &entity.User{Name: name, Email: strings.TrimSpace(email), Role: role}
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
	if email != "" {
		if existing, err := s.users.FindByEmail(ctx, email); err == nil && existing != nil {
			return nil, nil, fmt.Errorf("users: email already registered")
		}
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
	return s.users.Delete(ctx, userID)
}

func isAdmin(role string) bool { return role == entity.RoleAdmin }

// UserSummary is a user plus derived key counts for admin listing.
type UserSummary struct {
	*entity.User
	ActiveKeys int
}
