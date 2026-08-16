package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/handler"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newUsersTestServer(t *testing.T) (*hzserver.Hertz, *auth.Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	userRepo := magi.NewUserRepository(db)
	keyRepo := magi.NewApiKeyRepository(db)
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "admin", Key: "admin-key", UserID: 1, Role: "admin"}}).WithStores(keyRepo, userRepo)
	usersSvc := users.NewService(userRepo, keyRepo)
	usersH := handler.NewUsersHandler(usersSvc)

	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(authSvc))
	h.GET("/api/v1/me", usersH.Me)
	h.POST("/api/v1/me/keys", usersH.IssueOwnKey)
	h.POST("/api/v1/admin/users", server.RequireRole("admin"), usersH.CreateUser)
	h.GET("/api/v1/admin/users", server.RequireRole("admin"), usersH.ListUsers)
	h.POST("/api/v1/admin/users/:id/keys", server.RequireRole("admin"), usersH.IssueKey)
	h.GET("/api/v1/admin/users/:id/keys", server.RequireRole("admin"), usersH.ListKeys)
	h.POST("/api/v1/admin/keys/:id/revoke", server.RequireRole("admin"), usersH.RevokeKey)
	h.POST("/api/v1/admin/keys/:id/rotate", server.RequireRole("admin"), usersH.RotateKey)
	return h, authSvc
}

func bearer(token string) ut.Header {
	return ut.Header{Key: "Authorization", Value: "Bearer " + token}
}

func TestUsersAPI_AdminCreatesUserAndKeyAuthenticates(t *testing.T) {
	h, _ := newUsersTestServer(t)

	// Create user as admin.
	body := []byte(`{"name":"ci-runner","role":"user"}`)
	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/users",
		&ut.Body{Body: bytes.NewBuffer(body), Len: len(body)},
		bearer("admin-key"), ut.Header{Key: "Content-Type", Value: "application/json"})
	if w.Code != 201 {
		t.Fatalf("create user status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		User struct {
			ID         int64
			Name, Role string
		} `json:"user"`
		ApiKey struct{ Plaintext string } `json:"api_key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.User.Name != "ci-runner" || created.ApiKey.Plaintext == "" {
		t.Fatalf("created: %+v", created)
	}

	// The issued DB key must authenticate against /me.
	w = ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/me", nil, bearer(created.ApiKey.Plaintext))
	if w.Code != 200 {
		t.Fatalf("me with issued key status=%d body=%s", w.Code, w.Body.String())
	}
	var me struct {
		User struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("me unmarshal: %v", err)
	}
	if me.User.ID != created.User.ID || me.User.Role != "user" {
		t.Fatalf("me user: %+v", me.User)
	}

	// A non-admin (the new user's own key) must NOT be able to create users.
	w = ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/users",
		&ut.Body{Body: bytes.NewBuffer([]byte(`{"name":"x"}`)), Len: len([]byte(`{"name":"x"}`))},
		bearer(created.ApiKey.Plaintext), ut.Header{Key: "Content-Type", Value: "application/json"})
	if w.Code != 403 {
		t.Fatalf("non-admin create status=%d, want 403", w.Code)
	}

	// List keys for the new user.
	w = ut.PerformRequest(h.Engine, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d/keys", created.User.ID), nil, bearer("admin-key"))
	if w.Code != 200 {
		t.Fatalf("list keys status=%d body=%s", w.Code, w.Body.String())
	}
	var keys struct {
		Keys []struct{ ID, Prefix string } `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("keys unmarshal: %v", err)
	}
	if len(keys.Keys) != 1 {
		t.Fatalf("expected 1 key, got %+v", keys.Keys)
	}
	keyID := keys.Keys[0].ID

	// Revoke -> the key no longer authenticates.
	w = ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/keys/"+keyID+"/revoke", nil, bearer("admin-key"))
	if w.Code != 204 {
		t.Fatalf("revoke status=%d body=%s", w.Code, w.Body.String())
	}
	w = ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/me", nil, bearer(created.ApiKey.Plaintext))
	if w.Code != 401 {
		t.Fatalf("revoked key status=%d, want 401", w.Code)
	}
}

func TestUsersAPI_RotateKeyReturnsNewSecret(t *testing.T) {
	h, _ := newUsersTestServer(t)
	// create user
	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/users",
		&ut.Body{Body: bytes.NewBuffer([]byte(`{"name":"rot","role":"user"}`)), Len: len([]byte(`{"name":"rot","role":"user"}`))},
		bearer("admin-key"), ut.Header{Key: "Content-Type", Value: "application/json"})
	var created struct {
		User   struct{ ID int64 } `json:"user"`
		ApiKey struct {
			Plaintext string
			ID        string
		} `json:"api_key"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	rot := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/keys/"+created.ApiKey.ID+"/rotate", nil, bearer("admin-key"))
	if rot.Code != 200 {
		t.Fatalf("rotate status=%d body=%s", rot.Code, rot.Body.String())
	}
	var newKey struct{ Plaintext, ID string }
	_ = json.Unmarshal(rot.Body.Bytes(), &newKey)
	if newKey.Plaintext == "" || newKey.Plaintext == created.ApiKey.Plaintext {
		t.Fatalf("rotated key invalid: %+v", newKey)
	}
	// old key is now revoked
	w = ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/me", nil, bearer(created.ApiKey.Plaintext))
	if w.Code != 401 {
		t.Fatalf("old key after rotate status=%d, want 401", w.Code)
	}
}
