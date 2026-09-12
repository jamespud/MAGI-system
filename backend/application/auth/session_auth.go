package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

var (
	// ErrAuthStateUnavailable means the authoritative user state could not be
	// read. It is deliberately distinct from ErrUnauthenticated: the request
	// must fail closed (5xx), never fall back to the cookie's stale claims.
	ErrAuthStateUnavailable = errors.New("auth: user store unavailable")
)

// DefaultAuthStateTTL bounds how long a resolved authorization state is reused.
// The cache is a performance layer only: the user store remains the source of
// truth, and every permission-changing mutation on this process evicts the
// entry. A change made by another replica is not evicted here — it is bounded
// by this TTL (keep it short if cross-replica immediacy matters).
const DefaultAuthStateTTL = 45 * time.Second

type authState struct {
	name        string
	role        string
	active      bool
	authVersion int64
	expiresAt   time.Time
}

// SessionAuthorizer resolves the current authorization facts for a signed
// session. It never trusts the cookie for role or status: only the user id and
// the version the cookie was minted with come from the cookie.
type SessionAuthorizer struct {
	store port.UserRepository
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[int64]authState
	// gen bumps on every Invalidate. A lookup records the generation before it
	// reads the store and refuses to publish its result if the generation moved
	// meanwhile, so a read that raced a permission change cannot restore the
	// pre-change state into the cache.
	gen map[int64]uint64
}

// NewSessionAuthorizer builds an authorizer over the user repository. A
// non-positive ttl falls back to DefaultAuthStateTTL.
func NewSessionAuthorizer(store port.UserRepository, ttl time.Duration) *SessionAuthorizer {
	if ttl <= 0 {
		ttl = DefaultAuthStateTTL
	}
	return &SessionAuthorizer{
		store: store,
		ttl:   ttl,
		now:   time.Now,
		cache: make(map[int64]authState),
		gen:   make(map[int64]uint64),
	}
}

// Resolve returns the principal for a session that claims userID at
// authVersion, or an error the caller must honor.
func (a *SessionAuthorizer) Resolve(ctx context.Context, userID, authVersion int64) (*Principal, error) {
	if a == nil || a.store == nil {
		// No authoritative store is wired, so a session cannot be revalidated.
		return nil, ErrAuthStateUnavailable
	}
	state, err := a.state(ctx, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		// The account no longer exists.
		return nil, ErrSessionUnauthorized
	}
	if !state.active {
		return nil, ErrSessionUnauthorized
	}
	if state.authVersion != authVersion {
		return nil, ErrSessionUnauthorized
	}
	return &Principal{UserID: userID, Name: state.name, Role: state.role}, nil
}

// Invalidate drops the cached state for a user. Permission-changing mutations
// call this so the change takes effect immediately rather than after the TTL.
func (a *SessionAuthorizer) Invalidate(userID int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	delete(a.cache, userID)
	a.gen[userID]++
	a.mu.Unlock()
}

// InvalidateAll clears the whole cache (used by tests and bulk operations).
func (a *SessionAuthorizer) InvalidateAll() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.cache = make(map[int64]authState)
	a.gen = make(map[int64]uint64)
	a.mu.Unlock()
}

// stateReadRetries bounds how many times a lookup will re-read the store when a
// concurrent invalidation invalidated its result. Exhausting it fails closed
// rather than returning a value that may predate the change.
const stateReadRetries = 4

// state returns the cached or freshly loaded authorization state, or nil when
// the account does not exist. Storage failures are surfaced (never cached) so
// the caller can fail closed.
func (a *SessionAuthorizer) state(ctx context.Context, userID int64) (*authState, error) {
	for attempt := 0; ; attempt++ {
		now := a.now()
		a.mu.Lock()
		if cached, ok := a.cache[userID]; ok && now.Before(cached.expiresAt) {
			a.mu.Unlock()
			state := cached
			return &state, nil
		}
		gen := a.gen[userID]
		a.mu.Unlock()

		u, err := a.store.GetByID(ctx, userID)
		if err != nil {
			if errors.Is(err, port.ErrUserNotFound) {
				return nil, nil
			}
			return nil, ErrAuthStateUnavailable
		}
		if u == nil {
			return nil, nil
		}

		state := authState{
			name:        u.Name,
			role:        u.Role,
			active:      u.IsActive(),
			authVersion: u.AuthVersion,
			expiresAt:   now.Add(a.ttl),
		}
		a.mu.Lock()
		if a.gen[userID] != gen {
			// A permission change landed while this read was in flight, so the
			// value may predate it. Never publish it: re-read instead.
			a.mu.Unlock()
			if attempt >= stateReadRetries {
				return nil, ErrAuthStateUnavailable
			}
			continue
		}
		a.cache[userID] = state
		a.mu.Unlock()
		return &state, nil
	}
}
