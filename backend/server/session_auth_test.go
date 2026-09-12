package server_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server"
)

const sessionTestSecret = "abcdefghijklmnopqrstuvwxyz012345"

// stubUsers is a fixed-answer user store for the middleware tests.
type stubUsers struct {
	user *entity.User
	err  error
}

func (s stubUsers) Create(context.Context, *entity.User) error { return nil }
func (s stubUsers) GetByID(context.Context, int64) (*entity.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}
func (s stubUsers) FindByEmail(context.Context, string) (*entity.User, error) {
	return nil, port.ErrUserNotFound
}
func (s stubUsers) List(context.Context) ([]*entity.User, error) { return nil, nil }
func (s stubUsers) Update(context.Context, *entity.User) error   { return nil }
func (s stubUsers) Delete(context.Context, int64) error          { return nil }

func sessionServer(t *testing.T, repo port.UserRepository, codec *auth.SessionCodec) *hzserver.Hertz {
	t.Helper()
	authorizer := auth.NewSessionAuthorizer(repo, auth.DefaultAuthStateTTL)
	svc := auth.NewService(true, nil).
		WithStores(nil, repo).
		WithSession(codec).
		WithSessionAuthorizer(authorizer)
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/whoami", func(ctx context.Context, c *app.RequestContext) {
		p := auth.PrincipalFrom(ctx)
		if p == nil {
			c.JSON(401, map[string]any{"user": 0})
			return
		}
		c.JSON(200, map[string]any{"user": p.UserID, "role": p.Role})
	})
	return h
}

func performSession(h *hzserver.Hertz, token string) *ut.ResponseRecorder {
	return ut.PerformRequest(h.Engine, "GET", "/whoami", nil,
		ut.Header{Key: "Cookie", Value: "magi_session=" + token})
}

// TestSessionCookie_UsesStoredRole proves the middleware takes the role from
// the user store, not from the cookie.
func TestSessionCookie_UsesStoredRole(t *testing.T) {
	codec, err := auth.NewSessionCodec(sessionTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	repo := stubUsers{user: &entity.User{
		ID: 7, Name: "alice", Role: entity.RoleOperator,
		Status: entity.UserStatusActive, AuthVersion: 2,
	}}
	h := sessionServer(t, repo, codec)
	token, err := codec.Encode(7, 2)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	w := performSession(h, token)
	if w.Code != 200 {
		t.Fatalf("valid session: code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"role":"operator"`) || !strings.Contains(w.Body.String(), `"user":7`) {
		t.Fatalf("session body = %s", w.Body.String())
	}
}

func TestSessionCookie_StaleVersionIsRejected(t *testing.T) {
	codec, _ := auth.NewSessionCodec(sessionTestSecret, time.Hour)
	repo := stubUsers{user: &entity.User{
		ID: 7, Name: "alice", Role: entity.RoleOperator,
		Status: entity.UserStatusActive, AuthVersion: 3,
	}}
	h := sessionServer(t, repo, codec)
	token, _ := codec.Encode(7, 2) // minted before the version bump
	if w := performSession(h, token); w.Code != 401 {
		t.Fatalf("stale session: code=%d, want 401", w.Code)
	}
}

func TestSessionCookie_DisabledUserIsRejected(t *testing.T) {
	codec, _ := auth.NewSessionCodec(sessionTestSecret, time.Hour)
	repo := stubUsers{user: &entity.User{
		ID: 7, Name: "alice", Role: entity.RoleUser,
		Status: entity.UserStatusDisabled, AuthVersion: 2,
	}}
	h := sessionServer(t, repo, codec)
	token, _ := codec.Encode(7, 2)
	if w := performSession(h, token); w.Code != 401 {
		t.Fatalf("disabled user: code=%d, want 401", w.Code)
	}
}

func TestSessionCookie_DeletedUserIsRejected(t *testing.T) {
	codec, _ := auth.NewSessionCodec(sessionTestSecret, time.Hour)
	h := sessionServer(t, stubUsers{err: port.ErrUserNotFound}, codec)
	token, _ := codec.Encode(7, 2)
	if w := performSession(h, token); w.Code != 401 {
		t.Fatalf("deleted user: code=%d, want 401", w.Code)
	}
}

// TestSessionCookie_StoreFailureFailsClosed proves a storage outage returns
// 503 rather than honoring the cookie's stale authority.
func TestSessionCookie_StoreFailureFailsClosed(t *testing.T) {
	codec, _ := auth.NewSessionCodec(sessionTestSecret, time.Hour)
	h := sessionServer(t, stubUsers{err: errors.New("db down")}, codec)
	token, _ := codec.Encode(7, 2)
	w := performSession(h, token)
	if w.Code != 503 {
		t.Fatalf("store outage: code=%d body=%s, want 503", w.Code, w.Body.String())
	}
}
