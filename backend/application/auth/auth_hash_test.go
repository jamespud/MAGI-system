package auth_test

import (
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
	p, ok := svc.Authenticate(token)
	if !ok || p.UserID != 9 || p.Role != "admin" {
		t.Fatalf("authenticate hashed key: ok=%v p=%+v", ok, p)
	}
	if _, ok := svc.Authenticate("sk-live-wrong"); ok {
		t.Fatal("wrong token must not authenticate")
	}
}

func TestService_HashedKeyStillValidatesPlaintextFallback(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "legacy", Key: "sk-plain", UserID: 1, Role: "user"}})
	if _, ok := svc.Authenticate("sk-plain"); !ok {
		t.Fatal("plaintext key should still work")
	}
}
