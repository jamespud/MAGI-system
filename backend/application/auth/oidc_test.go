package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

type memOIDCUsers struct {
	mu      sync.Mutex
	byID    map[int64]*entity.User
	byEmail map[string]*entity.User
	next    int64
}

func (m *memOIDCUsers) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.byEmail[email]; ok {
		return u, nil
	}
	return nil, errNotFound
}

func (m *memOIDCUsers) Create(ctx context.Context, u *entity.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = m.next
	m.next++
	m.byID[u.ID] = u
	m.byEmail[u.Email] = u
	return nil
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (*notFoundError) Error() string { return "not found" }

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
			_ = json.NewEncoder(w).Encode(map[string]string{"sub": "sub-1", "email": "alice@example.com", "name": "Alice"})
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
	created, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "new@example.com", Name: "New"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if created.Role != entity.RoleUser || created.Email != "new@example.com" {
		t.Fatalf("created = %+v", created)
	}
	matched, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "new@example.com", Name: "New"})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if matched.ID != created.ID {
		t.Fatalf("expected same account, got %d vs %d", matched.ID, created.ID)
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
	if _, err := client.Provision(context.Background(), &OIDCIdentity{Sub: "s", Email: "x@example.com"}); err == nil {
		t.Fatal("unknown identity must be rejected without self-registration")
	}
}
