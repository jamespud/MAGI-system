package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// failingKeyStore fails key lookups with a non-not-found error.
type failingKeyStore struct{ err error }

func (f failingKeyStore) FindByKeyHash(context.Context, string) (*entity.ApiKey, error) {
	return nil, f.err
}
func (f failingKeyStore) Update(context.Context, *entity.ApiKey) error { return nil }

// failingUserStore fails owner lookups with a non-not-found error.
type failingUserStore struct{ err error }

func (f failingUserStore) GetByID(context.Context, int64) (*entity.User, error) {
	return nil, f.err
}

func strPtr(s string) *string { return &s }

// gateBeforeRead parks the owner lookup before it touches the store, so a test
// can land a disable ahead of the check.
type gateBeforeRead struct {
	inner   port.UserRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gateBeforeRead) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return g.inner.GetByID(ctx, id)
}

// gateAfterRead parks the owner lookup after it has read the row, so a test can
// land a disable behind the check (the in-flight window).
type gateAfterRead struct {
	inner   port.UserRepository
	read    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gateAfterRead) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	u, err := g.inner.GetByID(ctx, id)
	g.once.Do(func() { close(g.read) })
	<-g.release
	return u, err
}

func apiKeyFixture(t *testing.T) (*gorm.DB, port.UserRepository, port.ApiKeyRepository) {
	t.Helper()
	db := openUserDB(t)
	return db, magi.NewUserRepository(db), magi.NewApiKeyRepository(db)
}

func seedUser(t *testing.T, repo port.UserRepository, role string) *entity.User {
	t.Helper()
	u := &entity.User{Name: "alice", Email: "alice@example.com", Role: role}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func issueKey(t *testing.T, repo port.ApiKeyRepository, id string, userID int64) string {
	t.Helper()
	plain, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := repo.Create(context.Background(), &entity.ApiKey{
		ID: id, UserID: userID, Name: "cli", Prefix: prefix, KeyHash: hash,
	}); err != nil {
		t.Fatalf("persist key: %v", err)
	}
	return plain
}

func mutateUser(t *testing.T, repo port.UserRepository, id int64, m port.UserMutation) {
	t.Helper()
	writer, ok := repo.(port.AuthUserWriter)
	if !ok {
		t.Fatal("repo does not implement AuthUserWriter")
	}
	if _, err := writer.ApplyUserMutation(context.Background(), id, m); err != nil {
		t.Fatalf("apply mutation: %v", err)
	}
}

// TestAPIKey_UsesCurrentOwnerRole proves the role comes from the account, not
// from a value captured when the key was issued.
func TestAPIKey_UsesCurrentOwnerRole(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)
	svc := auth.NewService(true, nil).WithStores(keyRepo, userRepo)

	p, err := svc.Authenticate(context.Background(), plain)
	if err != nil || p.Role != entity.RoleUser {
		t.Fatalf("initial = %+v err=%v", p, err)
	}

	mutateUser(t, userRepo, u.ID, port.UserMutation{Role: strPtr(entity.RoleOperator), BumpAuthVersion: true})

	p, err = svc.Authenticate(context.Background(), plain)
	if err != nil || p.Role != entity.RoleOperator {
		t.Fatalf("role change must apply immediately: p=%+v err=%v", p, err)
	}
}

// TestAPIKey_DisabledOwnerRejected is the core fail-closed case: a disabled
// account must not keep authenticating through its API keys.
func TestAPIKey_DisabledOwnerRejected(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)
	svc := auth.NewService(true, nil).WithStores(keyRepo, userRepo)

	if _, err := svc.Authenticate(context.Background(), plain); err != nil {
		t.Fatalf("active owner should authenticate: %v", err)
	}
	mutateUser(t, userRepo, u.ID, port.UserMutation{Status: strPtr(entity.UserStatusDisabled), BumpAuthVersion: true})

	if _, err := svc.Authenticate(context.Background(), plain); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("disabled owner must be rejected, got %v", err)
	}
}

// TestAPIKey_DeletedOwnerRejected covers the orphaned-key case: deleting the
// account must not leave its keys authenticating.
func TestAPIKey_DeletedOwnerRejected(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleAdmin)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)
	svc := auth.NewService(true, nil).WithStores(keyRepo, userRepo)

	if err := userRepo.Delete(context.Background(), u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), plain); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("orphaned key must be rejected, got %v", err)
	}
}

func TestAPIKey_RevokedAndUnknownRejected(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)
	svc := auth.NewService(true, nil).WithStores(keyRepo, userRepo)

	key, err := keyRepo.GetByID(context.Background(), "ak-1")
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	key.Revoked = true
	if err := keyRepo.Update(context.Background(), key); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), plain); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("revoked key must be rejected, got %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), "mag_not-a-real-key"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("unknown key must be rejected, got %v", err)
	}
}

// TestAPIKey_StoreFailuresFailClosed pins that an unreadable key store or user
// store yields "unavailable" (503), never an authenticated principal.
func TestAPIKey_StoreFailuresFailClosed(t *testing.T) {
	boom := errors.New("db down")
	// Key store unreadable.
	keyFail := auth.NewService(true, nil).WithStores(failingKeyStore{err: boom}, failingUserStore{err: boom})
	if _, err := keyFail.Authenticate(context.Background(), "mag_whatever"); !errors.Is(err, auth.ErrAuthStateUnavailable) {
		t.Fatalf("key store failure = %v, want ErrAuthStateUnavailable", err)
	}

	// Key store readable, owner store unreadable.
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)
	userFail := auth.NewService(true, nil).WithStores(keyRepo, failingUserStore{err: boom})
	if _, err := userFail.Authenticate(context.Background(), plain); !errors.Is(err, auth.ErrAuthStateUnavailable) {
		t.Fatalf("user store failure = %v, want ErrAuthStateUnavailable", err)
	}
}

// TestAPIKey_StaticConfigKeyNeedsNoUserRow documents the boundary: keys
// declared in configuration carry their role inline and never consult the user
// store, so they are unaffected by DB availability.
func TestAPIKey_StaticConfigKeyNeedsNoUserRow(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "bootstrap", Key: "static-key", UserID: 1, Role: entity.RoleAdmin}}).
		WithStores(failingKeyStore{err: errors.New("down")}, failingUserStore{err: errors.New("down")})

	p, err := svc.Authenticate(context.Background(), "static-key")
	if err != nil || p.Role != entity.RoleAdmin {
		t.Fatalf("static config key = %+v err=%v", p, err)
	}
}

// TestAPIKey_DisableBeforeOwnerCheckIsRejected pins the ordering when the
// disable lands before the owner lookup: the request must be rejected.
func TestAPIKey_DisableBeforeOwnerCheckIsRejected(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)

	gate := &gateBeforeRead{inner: userRepo, entered: make(chan struct{}), release: make(chan struct{})}
	svc := auth.NewService(true, nil).WithStores(keyRepo, gate)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Authenticate(context.Background(), plain)
		done <- err
	}()
	<-gate.entered
	mutateUser(t, userRepo, u.ID, port.UserMutation{
		Status: strPtr(entity.UserStatusDisabled), BumpAuthVersion: true,
	})
	close(gate.release)

	if err := <-done; !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("disable landing before the owner check must reject, got %v", err)
	}
}

// TestAPIKey_DisableAfterOwnerCheckOnlyAffectsLaterRequests documents the commit
// point: authorization is evaluated when the credential is resolved, so a
// disable that lands after that read lets the already-in-flight request finish
// while every subsequent request re-checks and is rejected. This is a deliberate
// choice (no cross-request transaction), pinned here so it cannot drift.
func TestAPIKey_DisableAfterOwnerCheckOnlyAffectsLaterRequests(t *testing.T) {
	_, userRepo, keyRepo := apiKeyFixture(t)
	u := seedUser(t, userRepo, entity.RoleUser)
	plain := issueKey(t, keyRepo, "ak-1", u.ID)

	gate := &gateAfterRead{inner: userRepo, read: make(chan struct{}), release: make(chan struct{})}
	svc := auth.NewService(true, nil).WithStores(keyRepo, gate)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Authenticate(context.Background(), plain)
		done <- err
	}()
	<-gate.read // the owner row has been read while still active
	mutateUser(t, userRepo, u.ID, port.UserMutation{
		Status: strPtr(entity.UserStatusDisabled), BumpAuthVersion: true,
	})
	close(gate.release)

	if err := <-done; err != nil {
		t.Fatalf("in-flight request (checked before the disable) should complete: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), plain); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("the next request must be rejected, got %v", err)
	}
}
