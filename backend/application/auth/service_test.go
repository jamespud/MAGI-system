package auth_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/auth"
)

func TestAuthenticate_ValidAndInvalid(t *testing.T) {
	ctx := context.Background()
	svc := auth.NewService(true, []auth.KeySpec{
		{Name: "alice", Key: "secret-a", UserID: 1, Role: "admin"},
		{Name: "bob", Key: "secret-b", UserID: 2, Role: "user"},
	})
	p, ok := svc.Authenticate(ctx, "secret-b")
	if !ok || p == nil || p.UserID != 2 || p.Role != "user" {
		t.Fatalf("authenticate bob: %+v %v", p, ok)
	}
	if _, ok := svc.Authenticate(ctx, "wrong"); ok {
		t.Fatal("wrong key must fail")
	}
}

func TestAuthenticate_Disabled(t *testing.T) {
	ctx := context.Background()
	svc := auth.NewService(false, []auth.KeySpec{{Key: "k", UserID: 1}})
	if _, ok := svc.Authenticate(ctx, "k"); ok {
		t.Fatal("disabled service must reject")
	}
	if svc.Enabled() {
		t.Fatal("enabled flag mismatch")
	}
}

func TestBearerToken(t *testing.T) {
	if got := auth.BearerToken("Bearer tok123"); got != "tok123" {
		t.Fatalf("bearer: %q", got)
	}
	if got := auth.BearerToken("Basic abc"); got != "" {
		t.Fatalf("non-bearer: %q", got)
	}
	if got := auth.BearerToken(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestPrincipalContext(t *testing.T) {
	ctx := context.Background()
	if auth.PrincipalFrom(ctx) != nil {
		t.Fatal("no principal expected")
	}
	if !auth.CanAccess(ctx, 42) {
		t.Fatal("open mode must allow")
	}
	ctx = auth.WithPrincipal(ctx, &auth.Principal{UserID: 1})
	if !auth.CanAccess(ctx, 1) {
		t.Fatal("owner must pass")
	}
	if auth.CanAccess(ctx, 2) {
		t.Fatal("other owner must fail")
	}
}
