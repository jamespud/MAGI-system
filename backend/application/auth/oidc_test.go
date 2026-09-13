package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memOIDCUsers struct {
	mu      sync.Mutex
	byID    map[int64]*entity.User
	byEmail map[string]*entity.User
	next    int64
	// lookupErr, when set, simulates a storage failure on lookup.
	lookupErr error
	created   int
}

func (m *memOIDCUsers) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}
	if u, ok := m.byEmail[email]; ok {
		return u, nil
	}
	// Model the adapter contract: a missing account is reported with the
	// canonical sentinel so callers can tell it apart from a storage failure.
	return nil, port.ErrUserNotFound
}

func (m *memOIDCUsers) Create(ctx context.Context, u *entity.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created++
	u.ID = m.next
	m.next++
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

func (m *memOIDCUsers) FindByOIDCSubject(_ context.Context, sub string) (*entity.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}
	for _, u := range m.byID {
		if u.OIDCSubject != "" && u.OIDCSubject == sub {
			return u, nil
		}
	}
	return nil, port.ErrUserNotFound
}

func (m *memOIDCUsers) SetOIDCSubject(_ context.Context, userID int64, sub string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[userID]
	if !ok {
		return port.ErrUserNotFound
	}
	if u.OIDCSubject != "" && u.OIDCSubject != sub {
		return errors.New("already bound to another subject")
	}
	u.OIDCSubject = sub
	return nil
}

func newOIDCIssuer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "http://" + r.Host + "/authorize",
				"token_endpoint":         "http://" + r.Host + "/token",
				"userinfo_endpoint":      "http://" + r.Host + "/userinfo",
			})
		case "/authorize":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(r.URL.Query().Get("state")))
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "code-1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok-123", "token_type": "Bearer"})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sub": "sub-1", "email": "alice@example.com", "name": "Alice", "email_verified": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, srv.URL
}

func TestOIDCClient_AuthorizationURLAndExchange(t *testing.T) {
	srv, base := newOIDCIssuer(t)
	defer srv.Close()
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: base, ClientID: "cid", ClientSecret: "cs",
		RedirectURL: "http://localhost/auth/oidc/callback",
	}, &memOIDCUsers{byID: map[int64]*entity.User{}, byEmail: map[string]*entity.User{}, next: 1})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	authURL, err := client.AuthorizationURL("state-abc")
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}
	if !strings.Contains(authURL, "/authorize?") || !strings.Contains(authURL, "state=state-abc") ||
		!strings.Contains(authURL, "client_id=cid") {
		t.Fatalf("auth url = %s", authURL)
	}
	identity, err := client.Exchange(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if identity.Sub != "sub-1" || identity.Email != "alice@example.com" || identity.Name != "Alice" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestOIDCClient_ProvisionMatchesAndCreates(t *testing.T) {
	users := &memOIDCUsers{byID: map[int64]*entity.User{}, byEmail: map[string]*entity.User{}, next: 1}
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: true,
	}, users)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	created, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "new@example.com", Name: "New", EmailVerified: true})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if created.Role != entity.RoleUser || created.Email != "new@example.com" {
		t.Fatalf("created = %+v", created)
	}
	matched, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "new@example.com", Name: "New", EmailVerified: true})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if matched.ID != created.ID {
		t.Fatalf("expected same account, got %d vs %d", matched.ID, created.ID)
	}
}

// TestOIDCClient_ProvisionFailsClosedOnLookupError pins that a storage failure
// while matching the identity is NOT treated as "no such account": with
// self-registration enabled the old behavior would silently create a second
// account for an email it could not actually check.
func TestOIDCClient_ProvisionFailsClosedOnLookupError(t *testing.T) {
	boom := errors.New("db timeout")
	store := &memOIDCUsers{
		byID: map[int64]*entity.User{}, byEmail: map[string]*entity.User{}, next: 1,
		lookupErr: boom,
	}
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: true,
	}, store)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "new@example.com", EmailVerified: true}); !errors.Is(err, boom) {
		t.Fatalf("provision err = %v, want the lookup failure surfaced", err)
	}
	if store.created != 0 {
		t.Fatalf("provisioned %d accounts despite an unreadable user store", store.created)
	}
}

// TestOIDCClient_ProvisionRejectsUnverifiedEmail pins that an unverified
// address can never drive account discovery or provisioning.
func TestOIDCClient_ProvisionRejectsUnverifiedEmail(t *testing.T) {
	store := &memOIDCUsers{byID: map[int64]*entity.User{}, byEmail: map[string]*entity.User{}, next: 1}
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: true,
	}, store)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "a@b.c"}); err == nil {
		t.Fatal("provisioning accepted an unverified email")
	}
	if store.created != 0 {
		t.Fatalf("provisioned %d accounts for an unverified email", store.created)
	}
}

// TestOIDCClient_ProvisionBindsBySubjectNotEmail pins that a second subject
// cannot inherit an account by presenting the same (verified) email.
func TestOIDCClient_ProvisionBindsBySubjectNotEmail(t *testing.T) {
	existing := &entity.User{
		ID: 1, Name: "alice", Email: "a@b.c", Role: entity.RoleUser,
		Status: entity.UserStatusActive, OIDCSubject: "sub-1",
	}
	store := &memOIDCUsers{
		byID:    map[int64]*entity.User{1: existing},
		byEmail: map[string]*entity.User{"a@b.c": existing},
		next:    2,
	}
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: true,
	}, store)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "sub-2", Email: "a@b.c", EmailVerified: true}); err == nil {
		t.Fatal("a different subject was allowed to claim an existing account by email")
	}
	if store.created != 0 {
		t.Fatalf("provisioned %d accounts instead of rejecting the claim", store.created)
	}
}

// TestOIDCClient_ProvisionBackfillsLegacySubject pins the one-time adoption of
// accounts that predate the subject column.
func TestOIDCClient_ProvisionBackfillsLegacySubject(t *testing.T) {
	legacy := &entity.User{
		ID: 1, Name: "alice", Email: "a@b.c", Role: entity.RoleUser, Status: entity.UserStatusActive,
	}
	store := &memOIDCUsers{
		byID:    map[int64]*entity.User{1: legacy},
		byEmail: map[string]*entity.User{"a@b.c": legacy},
		next:    2,
	}
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: true,
	}, store)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "sub-9", Email: "a@b.c", EmailVerified: true})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got.OIDCSubject != "sub-9" {
		t.Fatalf("subject = %q, want sub-9 (legacy account must be backfilled)", got.OIDCSubject)
	}
	if store.created != 0 {
		t.Fatal("provisioned a new account instead of adopting the legacy one")
	}
}

func TestOIDCClient_RejectsUnknownWhenSelfRegistrationDisabled(t *testing.T) {
	client, err := NewOIDCClient(OIDCConfig{
		Enabled: true, Issuer: "https://issuer", ClientID: "cid",
		RedirectURL: "http://localhost/cb", SelfRegistration: false,
	}, &memOIDCUsers{byID: map[int64]*entity.User{}, byEmail: map[string]*entity.User{}, next: 1})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "x@example.com", EmailVerified: true}); err == nil {
		t.Fatal("unknown identity must be rejected without self-registration")
	}
}
