package users_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
)

type memUserRepo struct {
	byID map[int64]*entity.User
	next int64
}

func newMemUserRepo() *memUserRepo { return &memUserRepo{byID: map[int64]*entity.User{}, next: 1} }

func (r *memUserRepo) Create(ctx context.Context, u *entity.User) error {
	u.ID = r.next
	r.next++
	r.byID[u.ID] = u
	return nil
}
func (r *memUserRepo) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, errNotFound
}
func (r *memUserRepo) List(ctx context.Context) ([]*entity.User, error) {
	var out []*entity.User
	for _, u := range r.byID {
		out = append(out, u)
	}
	return out, nil
}
func (r *memUserRepo) Update(ctx context.Context, u *entity.User) error { return nil }
func (r *memUserRepo) Delete(ctx context.Context, id int64) error {
	delete(r.byID, id)
	return nil
}

type memKeyRepo struct {
	byID map[string]*entity.ApiKey
}

func newMemKeyRepo() *memKeyRepo { return &memKeyRepo{byID: map[string]*entity.ApiKey{}} }

func (r *memKeyRepo) Create(ctx context.Context, k *entity.ApiKey) error {
	r.byID[k.ID] = k
	return nil
}
func (r *memKeyRepo) GetByID(ctx context.Context, id string) (*entity.ApiKey, error) {
	if k, ok := r.byID[id]; ok {
		return k, nil
	}
	return nil, errNotFound
}
func (r *memKeyRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.ApiKey, error) {
	var out []*entity.ApiKey
	for _, k := range r.byID {
		if k.UserID == userID {
			out = append(out, k)
		}
	}
	return out, nil
}
func (r *memKeyRepo) FindByKeyHash(ctx context.Context, hash string) (*entity.ApiKey, error) {
	for _, k := range r.byID {
		if k.KeyHash == hash {
			return k, nil
		}
	}
	return nil, errNotFound
}
func (r *memKeyRepo) Update(ctx context.Context, k *entity.ApiKey) error {
	r.byID[k.ID] = k
	return nil
}
func (r *memKeyRepo) Delete(ctx context.Context, id string) error {
	delete(r.byID, id)
	return nil
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (*notFoundError) Error() string { return "not found" }

func TestUsersService_CreateUserAndAuthenticate(t *testing.T) {
	urepo := newMemUserRepo()
	krepo := newMemKeyRepo()
	svc := users.NewService(urepo, krepo)

	// Non-admin cannot create users.
	if _, _, err := svc.CreateUser(context.Background(), entity.RoleUser, "x", entity.RoleUser); err != users.ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}

	u, key, err := svc.CreateUser(context.Background(), entity.RoleAdmin, "alice", entity.RoleUser)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.Role != entity.RoleUser || key == nil || key.Plaintext == "" {
		t.Fatalf("user=%+v key=%+v", u, key)
	}

	// Admin may create an operator; operator cannot create users.
	op, _, err := svc.CreateUser(context.Background(), entity.RoleAdmin, "ops", entity.RoleOperator)
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}
	if op.Role != entity.RoleOperator {
		t.Fatalf("operator role = %s", op.Role)
	}
	if _, _, err := svc.CreateUser(context.Background(), entity.RoleOperator, "nested", entity.RoleUser); err != users.ErrForbidden {
		t.Fatalf("operator must not create users, got %v", err)
	}

	// The issued key authenticates via the auth service backed by these repos.
	authSvc := auth.NewService(true, nil).WithStores(krepo, urepo)
	p, ok := authSvc.Authenticate(context.Background(), key.Plaintext)
	if !ok || p.UserID != u.ID || p.Role != entity.RoleUser {
		t.Fatalf("auth issued key: ok=%v p=%+v", ok, p)
	}
}

func TestUsersService_IssueRevokeRotate(t *testing.T) {
	urepo := newMemUserRepo()
	krepo := newMemKeyRepo()
	svc := users.NewService(urepo, krepo)
	u, _, _ := svc.CreateUser(context.Background(), entity.RoleAdmin, "bob", entity.RoleUser)

	k1, err := svc.IssueKey(context.Background(), u.ID, entity.RoleUser, u.ID, "cli")
	if err != nil {
		t.Fatalf("issue own: %v", err)
	}
	// Non-admin cannot issue for another user.
	if _, err := svc.IssueKey(context.Background(), 999, entity.RoleUser, u.ID, "x"); err != users.ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}

	if err := svc.RevokeKey(context.Background(), u.ID, entity.RoleUser, k1.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	authSvc := auth.NewService(true, nil).WithStores(krepo, urepo)
	if _, ok := authSvc.Authenticate(context.Background(), k1.Plaintext); ok {
		t.Fatal("revoked key must not authenticate")
	}

	k2, err := svc.RotateKey(context.Background(), u.ID, entity.RoleUser, k1.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if k2.ID == k1.ID || k2.Plaintext == k1.Plaintext {
		t.Fatal("rotated key must differ")
	}
	if _, ok := authSvc.Authenticate(context.Background(), k2.Plaintext); !ok {
		t.Fatal("rotated key must authenticate")
	}
}

func TestUsersService_DeleteUserRemovesKeys(t *testing.T) {
	urepo := newMemUserRepo()
	krepo := newMemKeyRepo()
	svc := users.NewService(urepo, krepo)
	u, k, _ := svc.CreateUser(context.Background(), entity.RoleAdmin, "carol", entity.RoleAdmin)
	_ = k

	if err := svc.DeleteUser(context.Background(), entity.RoleUser, u.ID); err != users.ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if err := svc.DeleteUser(context.Background(), entity.RoleAdmin, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	keys, _ := krepo.ListByUser(context.Background(), u.ID)
	if len(keys) != 0 {
		t.Fatalf("expected keys removed, got %d", len(keys))
	}
}

func TestUsersService_MeAndList(t *testing.T) {
	urepo := newMemUserRepo()
	krepo := newMemKeyRepo()
	svc := users.NewService(urepo, krepo)
	u, key, _ := svc.CreateUser(context.Background(), entity.RoleAdmin, "dave", entity.RoleUser)

	me, keys, err := svc.Me(context.Background(), u.ID)
	if err != nil || me.ID != u.ID {
		t.Fatalf("me: %+v err=%v", me, err)
	}
	if len(keys) != 1 || keys[0].ID != key.ID {
		t.Fatalf("me keys: %+v", keys)
	}

	summaries, err := svc.ListUsers(context.Background(), entity.RoleAdmin)
	if err != nil || len(summaries) != 1 || summaries[0].ActiveKeys != 1 {
		t.Fatalf("list users: %+v err=%v", summaries, err)
	}
	if _, err := svc.ListUsers(context.Background(), entity.RoleUser); err != users.ErrForbidden {
		t.Fatalf("expected forbidden list, got %v", err)
	}
}
