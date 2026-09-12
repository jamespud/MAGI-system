package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jamespud/magi/backend/application/auth"
)

func TestService_AuthenticatesHashedKeys(t *testing.T) {
	token := "sk-live-123"
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	svc := auth.NewService(true, []auth.KeySpec{{Name: "ops", KeyHash: hash, UserID: 9, Role: "admin"}})
	p, err := svc.Authenticate(context.Background(), token)
	if err != nil || p.UserID != 9 || p.Role != "admin" {
		t.Fatalf("authenticate hashed key: err=%v p=%+v", err, p)
	}
	if _, err := svc.Authenticate(context.Background(), "sk-live-wrong"); err == nil {
		t.Fatal("wrong token must not authenticate")
	}
}

func TestService_HashedKeyStillValidatesPlaintextFallback(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "legacy", Key: "sk-plain", UserID: 1, Role: "user"}})
	if _, err := svc.Authenticate(context.Background(), "sk-plain"); err != nil {
		t.Fatal("plaintext key should still work")
	}
}
