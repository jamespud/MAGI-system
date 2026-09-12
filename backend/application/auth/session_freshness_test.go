package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

const testSecret = "abcdefghijklmnopqrstuvwxyz012345"

// countingUsers tracks lookups so a test can prove the auth-state cache is
// actually used, and that a permission change evicts it.
type countingUsers struct {
	port.UserRepository
	mu   sync.Mutex
	gets int
}

func (c *countingUsers) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.UserRepository.GetByID(ctx, id)
}

func (c *countingUsers) lookups() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets
}

type failingUsers struct {
	port.UserRepository
	err error
}

func (f failingUsers) GetByID(context.Context, int64) (*entity.User, error) { return nil, f.err }

type harness struct {
	repo     port.UserRepository
	counting *countingUsers
	svc      *users.Service
	codec    *auth.SessionCodec
	authz    *auth.SessionAuthorizer
	authSvc  *auth.Service
}

func openUserDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "users.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.UserModel{}, &magi.ApiKeyModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := openUserDB(t)
	repo := magi.NewUserRepository(db)
	keyRepo := magi.NewApiKeyRepository(db)
	counting := &countingUsers{UserRepository: repo}
	authorizer := auth.NewSessionAuthorizer(counting, auth.DefaultAuthStateTTL)
	codec, err := auth.NewSessionCodec(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	authSvc := auth.NewService(true, nil).
		WithStores(keyRepo, repo).
		WithSession(codec).
		WithSessionAuthorizer(authorizer)
	return &harness{
		repo:     repo,
		counting: counting,
		svc:      users.NewServiceWithOptions(repo, keyRepo, users.WithSessionInvalidator(authorizer)),
		codec:    codec,
		authz:    authorizer,
		authSvc:  authSvc,
	}
}

func (h *harness) seed(t *testing.T, role string) *entity.User {
	t.Helper()
	u := &entity.User{Name: "alice", Email: "alice@example.com", Role: role}
	if err := h.repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func (h *harness) cookieWithVersion(t *testing.T, u *entity.User, version int64) string {
	t.Helper()
	tok, err := h.codec.Encode(u.ID, version)
	if err != nil {
		t.Fatalf("encode cookie: %v", err)
	}
	return tok
}

func (h *harness) cookie(t *testing.T, u *entity.User) string {
	t.Helper()
	return h.cookieWithVersion(t, u, u.AuthVersion)
}

func (h *harness) authenticate(t *testing.T, token string) (*auth.Principal, error) {
	t.Helper()
	return h.authSvc.AuthenticateSession(context.Background(), token)
}

func (h *harness) reload(t *testing.T, id int64) *entity.User {
	t.Helper()
	u, err := h.repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return u
}

func ptr[T any](v T) *T { return &v }

func TestSessionFreshness_DemotionInvalidatesExistingCookie(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleAdmin)
	token := h.cookie(t, u)

	if p, err := h.authenticate(t, token); err != nil || p.Role != entity.RoleAdmin {
		t.Fatalf("initial session = %+v err = %v", p, err)
	}
	if _, err := h.svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Role: ptr(entity.RoleUser)}); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if _, err := h.authenticate(t, token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("cookie minted as admin must stop working after demotion, got %v", err)
	}
	fresh := h.cookie(t, h.reload(t, u.ID))
	if p, err := h.authenticate(t, fresh); err != nil || p.Role != entity.RoleUser {
		t.Fatalf("re-issued session = %+v err = %v", p, err)
	}
}

func TestSessionFreshness_DisableAndReEnable(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleUser)
	token := h.cookie(t, u)

	if _, err := h.svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Status: ptr(entity.UserStatusDisabled)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := h.authenticate(t, token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("disabled account must be rejected, got %v", err)
	}
	if _, err := h.svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Status: ptr(entity.UserStatusActive)}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := h.authenticate(t, token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("pre-disable cookie must not be revived by re-enable, got %v", err)
	}
	if p, err := h.authenticate(t, h.cookie(t, h.reload(t, u.ID))); err != nil || p == nil {
		t.Fatalf("session minted after re-enable = %+v err = %v", p, err)
	}
}

func TestSessionFreshness_RevokeAllInvalidatesCookie(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleUser)
	token := h.cookie(t, u)
	if _, err := h.authenticate(t, token); err != nil {
		t.Fatalf("warm session: %v", err)
	}
	if err := h.svc.RevokeSessions(context.Background(), entity.RoleAdmin, u.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := h.authenticate(t, token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("revoked session must be rejected, got %v", err)
	}
}

func TestSessionFreshness_ProfileEditKeepsSession(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleUser)
	token := h.cookie(t, u)
	before := h.reload(t, u.ID).AuthVersion

	// Warm the cache so the rename must evict for the new name to be visible.
	if p, err := h.authenticate(t, token); err != nil || p.Name != "alice" {
		t.Fatalf("warm session = %+v err = %v", p, err)
	}

	updated, err := h.svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Name: ptr("Alice Renamed")})
	if err != nil {
		t.Fatalf("profile update: %v", err)
	}
	if updated.AuthVersion != before {
		t.Fatalf("profile edit bumped auth_version %d -> %d; it must not", before, updated.AuthVersion)
	}
	p, err := h.authenticate(t, token)
	if err != nil {
		t.Fatalf("profile edit must not invalidate the session, got %v", err)
	}
	if p.Name != "Alice Renamed" {
		t.Fatalf("principal name = %q, want the updated profile name", p.Name)
	}
}

func TestSessionFreshness_CacheHitAndEvictionOnPermissionChange(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleAdmin)
	token := h.cookie(t, u)

	if _, err := h.authenticate(t, token); err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	afterFirst := h.counting.lookups()
	if _, err := h.authenticate(t, token); err != nil {
		t.Fatalf("cached authorize: %v", err)
	}
	if h.counting.lookups() != afterFirst {
		t.Fatalf("lookups grew on a cache hit: %d -> %d", afterFirst, h.counting.lookups())
	}

	if _, err := h.svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Role: ptr(entity.RoleOperator)}); err != nil {
		t.Fatalf("role change: %v", err)
	}
	if _, err := h.authenticate(t, token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("eviction must take effect immediately, got %v", err)
	}
	if h.counting.lookups() <= afterFirst {
		t.Fatal("expected the authorizer to re-read the store after eviction")
	}
}

func TestSessionFreshness_StoreFailureFailsClosed(t *testing.T) {
	store := failingUsers{err: errors.New("db down")}
	authorizer := auth.NewSessionAuthorizer(store, auth.DefaultAuthStateTTL)
	if _, err := authorizer.Resolve(context.Background(), 1, 0); !errors.Is(err, auth.ErrAuthStateUnavailable) {
		t.Fatalf("store failure = %v, want ErrAuthStateUnavailable (fail closed)", err)
	}
}

func TestSessionFreshness_MissingUserAndWrongVersionRejected(t *testing.T) {
	h := newHarness(t)
	u := h.seed(t, entity.RoleUser)

	if _, err := h.authenticate(t, h.cookieWithVersion(t, u, 99)); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("forged auth version must be rejected, got %v", err)
	}
	if err := h.svc.DeleteUser(context.Background(), entity.RoleAdmin, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.authenticate(t, h.cookie(t, u)); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("deleted account session must be rejected, got %v", err)
	}
}

func TestSessionFreshness_RejectsMalformedAndExpiredCookie(t *testing.T) {
	h := newHarness(t)
	if _, err := h.authenticate(t, "not-a-token"); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("malformed cookie = %v, want ErrSessionUnauthorized", err)
	}
	expired, err := auth.NewSessionCodec(testSecret, time.Millisecond)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	tok, err := expired.Encode(1, 0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := h.authenticate(t, tok); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("expired cookie = %v, want ErrSessionUnauthorized", err)
	}
}

// gatedUsers blocks the first store lookup *after* it has read the row, so a
// test can deterministically interleave a permission change with an in-flight
// authorization read.
type gatedUsers struct {
	port.UserRepository
	read    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gatedUsers) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	u, err := g.UserRepository.GetByID(ctx, id)
	g.once.Do(func() { close(g.read) })
	<-g.release
	return u, err
}

// TestSessionFreshness_InvalidateDuringInflightReadIsNotRefilled pins the
// linearization point: a lookup that read the pre-change row must not restore
// it into the cache after Invalidate ran, or a revoked cookie would keep
// working for a full TTL.
func TestSessionFreshness_InvalidateDuringInflightReadIsNotRefilled(t *testing.T) {
	db := openUserDB(t)
	repo := magi.NewUserRepository(db)
	gated := &gatedUsers{
		UserRepository: repo,
		read:           make(chan struct{}),
		release:        make(chan struct{}),
	}
	authorizer := auth.NewSessionAuthorizer(gated, auth.DefaultAuthStateTTL)
	codec, err := auth.NewSessionCodec(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	authSvc := auth.NewService(true, nil).WithSession(codec).WithSessionAuthorizer(authorizer)
	userSvc := users.NewServiceWithOptions(repo, magi.NewApiKeyRepository(db), users.WithSessionInvalidator(authorizer))

	u := &entity.User{Name: "alice", Email: "alice@example.com", Role: entity.RoleAdmin}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := codec.Encode(u.ID, u.AuthVersion)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	type result struct {
		p   *auth.Principal
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := authSvc.AuthenticateSession(context.Background(), token)
		done <- result{p: p, err: err}
	}()

	// The in-flight lookup has now read the pre-change (admin) row.
	<-gated.read
	// The permission change completes and invalidates while that read is parked.
	if _, err := userSvc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{Role: ptr(entity.RoleUser)}); err != nil {
		t.Fatalf("demote: %v", err)
	}
	close(gated.release)

	got := <-done
	if !errors.Is(got.err, auth.ErrSessionUnauthorized) {
		t.Fatalf("stale in-flight read authorized a revoked cookie: p=%+v err=%v", got.p, got.err)
	}
	// If the stale value had been refilled, this second call would be served
	// from cache and wrongly succeed.
	if p, err := authSvc.AuthenticateSession(context.Background(), token); !errors.Is(err, auth.ErrSessionUnauthorized) {
		t.Fatalf("stale state was refilled into the cache: p=%+v err=%v", p, err)
	}
}
