package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserAndApiKeyRepository_Lifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.UserModel{}, &magi.ApiKeyModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users := magi.NewUserRepository(db)
	keys := magi.NewApiKeyRepository(db)

	u := &entity.User{Name: "alice", Role: entity.RoleUser}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected generated user id")
	}

	got, err := users.GetByID(context.Background(), u.ID)
	if err != nil || got.Name != "alice" {
		t.Fatalf("get user: %+v err=%v", got, err)
	}

	plain, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	k := &entity.ApiKey{ID: "ak-1", UserID: u.ID, Name: "cli", Prefix: prefix, KeyHash: hash}
	if err := keys.Create(context.Background(), k); err != nil {
		t.Fatalf("create key: %v", err)
	}

	found, err := keys.FindByKeyHash(context.Background(), auth.HashToken(plain))
	if err != nil || found == nil || found.ID != "ak-1" {
		t.Fatalf("find by hash: %+v err=%v", found, err)
	}

	// Revoke + update persists.
	found.Revoked = true
	if err := keys.Update(context.Background(), found); err != nil {
		t.Fatalf("update key: %v", err)
	}
	gotKey, _ := keys.GetByID(context.Background(), "ak-1")
	if !gotKey.Revoked {
		t.Fatal("expected key revoked")
	}

	// Auth must reject a revoked key and accept a live one.
	authSvc := auth.NewService(true, nil).WithStores(keys, users)
	if _, ok := authSvc.Authenticate(context.Background(), plain); ok {
		t.Fatal("revoked key must not authenticate")
	}
	plain2, _, hash2, _ := auth.GenerateAPIKey()
	_ = keys.Create(context.Background(), &entity.ApiKey{ID: "ak-2", UserID: u.ID, Name: "cli2", Prefix: "p", KeyHash: hash2})
	if _, ok := authSvc.Authenticate(context.Background(), plain2); !ok {
		t.Fatal("live key must authenticate")
	}

	// Delete user removes rows.
	_ = keys.Delete(context.Background(), "ak-1")
	_ = keys.Delete(context.Background(), "ak-2")
	if err := users.Delete(context.Background(), u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := users.GetByID(context.Background(), u.ID); err == nil {
		t.Fatal("expected user deleted")
	}
}
