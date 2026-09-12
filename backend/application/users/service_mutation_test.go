package users_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func openMutationDB(t *testing.T) *gorm.DB {
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

func seedMutationUser(t *testing.T, repo port.UserRepository, role string) *entity.User {
	t.Helper()
	u := &entity.User{Name: "bob", Email: "bob@example.com", Role: role}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func reloadUser(t *testing.T, repo port.UserRepository, id int64) *entity.User {
	t.Helper()
	u, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return u
}

// TestUpdateUser_InvalidFieldLeavesNoPartialChange pins that the whole patch is
// validated before any write, so a valid role combined with an invalid status
// must not promote the account.
func TestUpdateUser_InvalidFieldLeavesNoPartialChange(t *testing.T) {
	db := openMutationDB(t)
	repo := magi.NewUserRepository(db)
	svc := users.NewService(repo, magi.NewApiKeyRepository(db))
	u := seedMutationUser(t, repo, entity.RoleUser)
	before := reloadUser(t, repo, u.ID)

	_, err := svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{
		Role:   ptrValue(entity.RoleAdmin),
		Status: ptrValue("sleeping"),
	})
	if err == nil {
		t.Fatal("invalid status must be rejected")
	}
	after := reloadUser(t, repo, u.ID)
	if after.Role != before.Role {
		t.Fatalf("role changed despite an invalid patch: %q -> %q", before.Role, after.Role)
	}
	if after.AuthVersion != before.AuthVersion {
		t.Fatalf("auth_version bumped despite an invalid patch: %d -> %d", before.AuthVersion, after.AuthVersion)
	}
	if after.Status != before.Status {
		t.Fatalf("status changed despite an invalid patch: %q -> %q", before.Status, after.Status)
	}
}

// TestUpdateUser_RoleAndStatusBumpVersionExactlyOnce pins that one patch is one
// mutation, not two sequential ones.
func TestUpdateUser_RoleAndStatusBumpVersionExactlyOnce(t *testing.T) {
	db := openMutationDB(t)
	repo := magi.NewUserRepository(db)
	svc := users.NewService(repo, magi.NewApiKeyRepository(db))
	u := seedMutationUser(t, repo, entity.RoleUser)
	before := reloadUser(t, repo, u.ID)

	updated, err := svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{
		Role:   ptrValue(entity.RoleOperator),
		Status: ptrValue(entity.UserStatusDisabled),
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if updated.Role != entity.RoleOperator || updated.Status != entity.UserStatusDisabled {
		t.Fatalf("updated = role %q status %q", updated.Role, updated.Status)
	}
	if updated.AuthVersion != before.AuthVersion+1 {
		t.Fatalf("auth_version = %d, want exactly one bump from %d", updated.AuthVersion, before.AuthVersion)
	}
}

// failingWriter fails the mutation so the test can assert no partial state is
// left behind when the store rejects the write.
type failingWriter struct {
	port.UserRepository
	err error
}

func (f failingWriter) ApplyUserMutation(context.Context, int64, port.UserMutation) (*entity.User, error) {
	return nil, f.err
}
func (f failingWriter) BumpAuthVersion(context.Context, int64) (int64, error) {
	return 0, f.err
}

func TestUpdateUser_MutationErrorLeavesNoPartialState(t *testing.T) {
	db := openMutationDB(t)
	repo := magi.NewUserRepository(db)
	userErr := errors.New("db write failed")
	svc := users.NewService(failingWriter{UserRepository: repo, err: userErr}, magi.NewApiKeyRepository(db))
	u := seedMutationUser(t, repo, entity.RoleUser)
	before := reloadUser(t, repo, u.ID)

	_, err := svc.UpdateUser(context.Background(), entity.RoleAdmin, u.ID, users.UserPatch{
		Role: ptrValue(entity.RoleAdmin),
	})
	if !errors.Is(err, userErr) {
		t.Fatalf("err = %v, want the storage error surfaced", err)
	}
	after := reloadUser(t, repo, u.ID)
	if after.Role != before.Role || after.AuthVersion != before.AuthVersion {
		t.Fatalf("partial state after a failed mutation: role %q -> %q, version %d -> %d",
			before.Role, after.Role, before.AuthVersion, after.AuthVersion)
	}
}

// lookupFailRepo fails email lookups with a non-not-found error.
type lookupFailRepo struct {
	port.UserRepository
	err error
}

func (l lookupFailRepo) FindByEmail(context.Context, string) (*entity.User, error) {
	return nil, l.err
}

// TestSelfRegister_LookupFailureFailsClosed pins that a lookup outage is not
// treated as "email not registered" (which would mint a duplicate account).
func TestSelfRegister_LookupFailureFailsClosed(t *testing.T) {
	db := openMutationDB(t)
	repo := magi.NewUserRepository(db)
	boom := errors.New("db timeout")
	svc := users.NewServiceWithOptions(
		lookupFailRepo{UserRepository: repo, err: boom},
		magi.NewApiKeyRepository(db),
		users.WithSelfRegistration(true),
	)
	if _, _, err := svc.SelfRegister(context.Background(), "carol", "carol@example.com"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the lookup failure surfaced", err)
	}
	if _, err := repo.FindByEmail(context.Background(), "carol@example.com"); !errors.Is(err, port.ErrUserNotFound) {
		t.Fatalf("a user was created despite the lookup failure: %v", err)
	}
}

func ptrValue[T any](v T) *T { return &v }
