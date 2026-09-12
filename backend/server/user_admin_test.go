package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/handler"
)

// newUserAdminServer wires the admin user endpoints plus the session
// invalidator, so the tests exercise the same path production uses.
func newUserAdminServer(t *testing.T) (*hzserver.Hertz, port.UserRepository) {
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
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	userRepo := magi.NewUserRepository(db)
	keyRepo := magi.NewApiKeyRepository(db)
	authorizer := auth.NewSessionAuthorizer(userRepo, auth.DefaultAuthStateTTL)
	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "admin", Key: "admin-key", UserID: 1, Role: "admin"},
	}).WithStores(keyRepo, userRepo)
	usersSvc := users.NewServiceWithOptions(userRepo, keyRepo, users.WithSessionInvalidator(authorizer))
	usersH := handler.NewUsersHandler(usersSvc)

	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(authSvc))
	h.POST("/api/v1/admin/users", server.RequireRole("admin"), usersH.CreateUser)
	h.PATCH("/api/v1/admin/users/:id", server.RequireRole("admin"), usersH.UpdateUser)
	h.POST("/api/v1/admin/users/:id/revoke-sessions", server.RequireRole("admin"), usersH.RevokeSessions)
	return h, userRepo
}

func createUserViaAPI(t *testing.T, h *hzserver.Hertz, name, role string) int64 {
	t.Helper()
	body := []byte(`{"name":"` + name + `","role":"` + role + `"}`)
	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/users",
		&ut.Body{Body: bytes.NewBuffer(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"}, bearer("admin-key"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp.User.ID
}

func patchUser(t *testing.T, h *hzserver.Hertz, id int64, payload string) *ut.ResponseRecorder {
	t.Helper()
	body := []byte(payload)
	return ut.PerformRequest(h.Engine, http.MethodPatch, "/api/v1/admin/users/"+strconv.FormatInt(id, 10),
		&ut.Body{Body: bytes.NewBuffer(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"}, bearer("admin-key"))
}

// TestAdminUserAPI_RoleChangeBumpsVersionAndProfileEditDoesNot pins the
// session-invalidation policy at the HTTP boundary.
func TestAdminUserAPI_RoleChangeBumpsVersionAndProfileEditDoesNot(t *testing.T) {
	h, repo := newUserAdminServer(t)
	id := createUserViaAPI(t, h, "bob", "user")

	before, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}

	// Profile-only edit: no version bump.
	w := patchUser(t, h, id, `{"name":"bob renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("profile patch: code=%d body=%s", w.Code, w.Body.String())
	}
	afterProfile, _ := repo.GetByID(context.Background(), id)
	if afterProfile.AuthVersion != before.AuthVersion {
		t.Fatalf("profile edit bumped auth_version %d -> %d", before.AuthVersion, afterProfile.AuthVersion)
	}
	if afterProfile.Name != "bob renamed" {
		t.Fatalf("name = %q", afterProfile.Name)
	}

	// Role change: version must advance.
	w = patchUser(t, h, id, `{"role":"operator"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("role patch: code=%d body=%s", w.Code, w.Body.String())
	}
	afterRole, _ := repo.GetByID(context.Background(), id)
	if afterRole.Role != "operator" {
		t.Fatalf("role = %q, want operator", afterRole.Role)
	}
	if afterRole.AuthVersion <= afterProfile.AuthVersion {
		t.Fatalf("role change must bump auth_version: %d -> %d", afterProfile.AuthVersion, afterRole.AuthVersion)
	}
}

func TestAdminUserAPI_DisableAndRevokeSessionsBumpVersion(t *testing.T) {
	h, repo := newUserAdminServer(t)
	id := createUserViaAPI(t, h, "carol", "user")
	start, _ := repo.GetByID(context.Background(), id)

	if w := patchUser(t, h, id, `{"status":"disabled"}`); w.Code != http.StatusOK {
		t.Fatalf("disable: code=%d body=%s", w.Code, w.Body.String())
	}
	disabled, _ := repo.GetByID(context.Background(), id)
	if disabled.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", disabled.Status)
	}
	if disabled.AuthVersion <= start.AuthVersion {
		t.Fatalf("disable must bump auth_version: %d -> %d", start.AuthVersion, disabled.AuthVersion)
	}

	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/users/"+strconv.FormatInt(id, 10)+"/revoke-sessions",
		nil, bearer("admin-key"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke-sessions: code=%d body=%s", w.Code, w.Body.String())
	}
	revoked, _ := repo.GetByID(context.Background(), id)
	if revoked.AuthVersion <= disabled.AuthVersion {
		t.Fatalf("revoke-sessions must bump auth_version: %d -> %d", disabled.AuthVersion, revoked.AuthVersion)
	}
}

func TestAdminUserAPI_RejectsInvalidRoleAndStatus(t *testing.T) {
	h, _ := newUserAdminServer(t)
	id := createUserViaAPI(t, h, "dave", "user")
	if w := patchUser(t, h, id, `{"role":"superuser"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid role: code=%d, want 400", w.Code)
	}
	if w := patchUser(t, h, id, `{"status":"sleeping"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: code=%d, want 400", w.Code)
	}
}
