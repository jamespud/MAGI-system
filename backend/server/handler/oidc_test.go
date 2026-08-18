package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/handler"
)

type memRepo struct {
	users map[int64]*entity.User
	next  int64
}

func (m *memRepo) Create(ctx context.Context, u *entity.User) error {
	u.ID = m.next
	m.next++
	m.users[u.ID] = u
	return nil
}
func (m *memRepo) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	return m.users[id], nil
}
func (m *memRepo) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, errNotFound
}
func (m *memRepo) List(ctx context.Context) ([]*entity.User, error) { return nil, nil }
func (m *memRepo) Update(ctx context.Context, u *entity.User) error { return nil }
func (m *memRepo) Delete(ctx context.Context, id int64) error       { return nil }

type memKeyRepo struct {
	keys map[string]*entity.ApiKey
}

func (m *memKeyRepo) Create(ctx context.Context, k *entity.ApiKey) error {
	m.keys[k.ID] = k
	return nil
}
func (m *memKeyRepo) GetByID(ctx context.Context, id string) (*entity.ApiKey, error) {
	return m.keys[id], nil
}
func (m *memKeyRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.ApiKey, error) {
	return nil, nil
}
func (m *memKeyRepo) FindByKeyHash(ctx context.Context, hash string) (*entity.ApiKey, error) {
	return nil, errNotFound
}
func (m *memKeyRepo) Update(ctx context.Context, k *entity.ApiKey) error { return nil }
func (m *memKeyRepo) Delete(ctx context.Context, id string) error        { return nil }

var errNotFound = &notFound{}

type notFound struct{}

func (*notFound) Error() string { return "not found" }

func newIssuer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "http://" + r.Host + "/authorize",
				"token_endpoint":         "http://" + r.Host + "/token",
				"userinfo_endpoint":      "http://" + r.Host + "/userinfo",
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]string{"sub": "s", "email": "oidc@example.com", "name": "OIDC User"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func newOIDCStack(t *testing.T) (*handler.OIDCHandler, *memRepo) {
	t.Helper()
	issuer := newIssuer(t)
	t.Cleanup(issuer.Close)
	repo := &memRepo{users: map[int64]*entity.User{}, next: 1}
	client, err := auth.NewOIDCClient(auth.OIDCConfig{
		Enabled: true, Issuer: issuer.URL, ClientID: "cid", ClientSecret: "cs",
		RedirectURL: "http://localhost/auth/oidc/callback", SelfRegistration: true,
	}, repo)
	if err != nil {
		t.Fatalf("oidc client: %v", err)
	}
	codec := auth.NewSessionCodec("secret", time.Hour)
	usersSvc := users.NewServiceWithOptions(repo, &memKeyRepo{keys: map[string]*entity.ApiKey{}}, users.WithSelfRegistration(true))
	return handler.NewOIDCHandler(client, codec, usersSvc, nil), repo
}

func TestOIDCHandler_LoginRedirectsAndCallbackSetsCookie(t *testing.T) {
	oidc, repo := newOIDCStack(t)
	h := server.Default(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/login", oidc.Login)
	h.GET("/callback", oidc.Callback)

	w := ut.PerformRequest(h.Engine, "GET", "/login", nil)
	if w.Code != 302 {
		t.Fatalf("login status = %d body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/authorize?") || !strings.Contains(loc, "state=") {
		t.Fatalf("location = %s", loc)
	}
	state := loc[strings.Index(loc, "state=")+len("state="):]

	w = ut.PerformRequest(h.Engine, "GET", "/callback?state="+state+"&code=abc", nil)
	if w.Code != 302 {
		t.Fatalf("callback status = %d body=%s", w.Code, w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "magi_session=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("cookie = %q", cookie)
	}
	if _, ok := repo.users[1]; !ok || repo.users[1].Email != "oidc@example.com" {
		t.Fatalf("provisioned user = %+v", repo.users)
	}
}

func TestOIDCHandler_RegisterDisabledForbidden(t *testing.T) {
	issuer := newIssuer(t)
	defer issuer.Close()
	repo := &memRepo{users: map[int64]*entity.User{}, next: 1}
	client, err := auth.NewOIDCClient(auth.OIDCConfig{
		Enabled: true, Issuer: issuer.URL, ClientID: "cid", RedirectURL: "http://localhost/cb",
	}, repo)
	if err != nil {
		t.Fatalf("oidc client: %v", err)
	}
	usersSvc := users.NewServiceWithOptions(repo, &memKeyRepo{keys: map[string]*entity.ApiKey{}}, users.WithSelfRegistration(false))
	oidc := handler.NewOIDCHandler(client, auth.NewSessionCodec("s", time.Hour), usersSvc, nil)
	h := server.Default(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/register", oidc.Register)
	w := ut.PerformRequest(h.Engine, "POST", "/register",
		&ut.Body{Body: strings.NewReader(`{"name":"x"}`), Len: len(`{"name":"x"}`)},
		ut.Header{Key: "Content-Type", Value: "application/json"})
	if w.Code != 403 {
		t.Fatalf("register status = %d, want 403", w.Code)
	}
}

var _ port.UserRepository = (*memRepo)(nil)
var _ port.ApiKeyRepository = (*memKeyRepo)(nil)
