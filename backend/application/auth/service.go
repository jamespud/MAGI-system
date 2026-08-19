package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Principal is the authenticated caller identity for a request.
type Principal struct {
	UserID int64
	Name   string
	Role   string
}

// KeySpec is one API key entry from configuration.
type KeySpec struct {
	Name    string
	Key     string
	KeyHash string
	UserID  int64
	Role    string
}

// KeyStore is the runtime (DB-backed) API-key store consulted after the
// static configuration keys. Implemented by adapter.ApiKeyRepository.
type KeyStore interface {
	FindByKeyHash(ctx context.Context, hash string) (*entity.ApiKey, error)
	Update(ctx context.Context, k *entity.ApiKey) error
}

// UserStore resolves a runtime user's role for DB-backed keys. Implemented by
// adapter.UserRepository.
type UserStore interface {
	GetByID(ctx context.Context, id int64) (*entity.User, error)
}

// Service authenticates bearer tokens against configured API keys plus, when
// wired, DB-backed runtime keys. When disabled, Authenticate always fails and
// callers fall back to open mode.
type Service struct {
	enabled   bool
	keys      map[string]Principal
	hashes    map[string]Principal
	keyStore  KeyStore
	userStore UserStore
	session   *SessionCodec
}

// WithStores wires the DB-backed key/user stores so runtime-issued keys
// authenticate in addition to the static configuration keys.
func (s *Service) WithStores(keys KeyStore, users UserStore) *Service {
	s.keyStore = keys
	s.userStore = users
	return s
}

// WithSession enables signed-cookie session authentication.
func (s *Service) WithSession(codec *SessionCodec) *Service {
	s.session = codec
	return s
}

// AuthenticateSession validates a signed session cookie token.
func (s *Service) AuthenticateSession(token string) (*Principal, bool) {
	if !s.enabled || s.session == nil || token == "" {
		return nil, false
	}
	p, err := s.session.Decode(token)
	if err != nil {
		return nil, false
	}
	return p, true
}

func NewService(enabled bool, keys []KeySpec) *Service {
	index := make(map[string]Principal, len(keys))
	hashes := make(map[string]Principal, len(keys))
	for _, k := range keys {
		if k.Key != "" {
			index[k.Key] = Principal{UserID: k.UserID, Name: k.Name, Role: k.Role}
		} else if k.KeyHash != "" {
			hashes[k.KeyHash] = Principal{UserID: k.UserID, Name: k.Name, Role: k.Role}
		}
	}
	return &Service{enabled: enabled, keys: index, hashes: hashes}
}

func (s *Service) Enabled() bool { return s.enabled }

// Authenticate returns the principal for a token using constant-time
// comparison to avoid API-key timing side channels. Static configuration keys
// are checked first; DB-backed runtime keys (by SHA-256 hash) are checked
// second, recording last-used time best-effort.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, bool) {
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
	if token != "" && (len(s.hashes) > 0 || s.keyStore != nil) {
		hash := HashToken(token)
		for h, p := range s.hashes {
			if len(h) != len(hash) {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(h), []byte(hash)) == 1 {
				cp := p
				return &cp, true
			}
		}
		if s.keyStore != nil {
			if key, err := s.keyStore.FindByKeyHash(ctx, hash); err == nil && key != nil && !key.Revoked {
				role := entity.RoleUser
				if u, uerr := s.userStore.GetByID(ctx, key.UserID); uerr == nil && u != nil && u.Role != "" {
					role = u.Role
				}
				now := time.Now()
				key.LastUsedAt = &now
				_ = s.keyStore.Update(ctx, key) // best-effort observability
				return &Principal{UserID: key.UserID, Name: key.Name, Role: role}, true
			}
		}
	}
	return nil, false
}

// HashToken returns the SHA-256 hex digest used to store and look up keys.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateAPIKey creates a random 256-bit key with a "mag_" prefix. Returns
// the plaintext (shown once), its display prefix, and its SHA-256 hash.
func GenerateAPIKey() (plaintext, prefix, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	plaintext = "mag_" + raw
	if len(raw) > 8 {
		prefix = plaintext[:len("mag_")+8]
	} else {
		prefix = plaintext
	}
	return plaintext, prefix, HashToken(plaintext), nil
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
// A nil principal is the unauthenticated open mode where every resource is
// readable. An ownerID of 0 means the resource is unowned (legacy cases
// created before multi-tenant ownership was enforced): when authenticated
// those are only visible to admins, closing the "unowned = world-readable"
// multi-tenant leak.
func CanAccess(ctx context.Context, ownerID int64) bool {
	p := PrincipalFrom(ctx)
	if p == nil {
		return true
	}
	if ownerID == 0 {
		return p.Role == "admin"
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
