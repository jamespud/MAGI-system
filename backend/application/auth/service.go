package auth

import (
	"context"
	"crypto/subtle"
	"strings"
)

// Principal is the authenticated caller identity for a request.
type Principal struct {
	UserID int64
	Name   string
	Role   string
}

// KeySpec is one API key entry from configuration.
type KeySpec struct {
	Name   string
	Key    string
	UserID int64
	Role   string
}

// Service authenticates bearer tokens against configured API keys. When
// disabled, Authenticate always fails and callers fall back to open mode.
type Service struct {
	enabled bool
	keys    map[string]Principal
}

func NewService(enabled bool, keys []KeySpec) *Service {
	index := make(map[string]Principal, len(keys))
	for _, k := range keys {
		if k.Key == "" {
			continue
		}
		index[k.Key] = Principal{UserID: k.UserID, Name: k.Name, Role: k.Role}
	}
	return &Service{enabled: enabled, keys: index}
}

func (s *Service) Enabled() bool { return s.enabled }

// Authenticate returns the principal for a token using constant-time
// comparison to avoid API-key timing side channels.
func (s *Service) Authenticate(token string) (*Principal, bool) {
	if !s.enabled || token == "" {
		return nil, false
	}
	for key, p := range s.keys {
		if len(key) != len(token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
			cp := p
			return &cp, true
		}
	}
	return nil, false
}

type principalKey struct{}

// WithPrincipal attaches a principal to the request context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom reads the principal attached by the auth middleware.
func PrincipalFrom(ctx context.Context) *Principal {
	if p, ok := ctx.Value(principalKey{}).(*Principal); ok {
		return p
	}
	return nil
}

// CanAccess reports whether a principal may access an owner-scoped resource.
// Owner 0 means the resource is unowned (open mode); a nil principal is the
// unauthenticated open mode where every resource is readable.
func CanAccess(ctx context.Context, ownerID int64) bool {
	p := PrincipalFrom(ctx)
	if p == nil || ownerID == 0 {
		return true
	}
	return p.UserID == ownerID
}

// BearerToken extracts the token from an Authorization header.
func BearerToken(header string) string {
	h := strings.TrimSpace(header)
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
