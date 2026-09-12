package auth

import (
	"encoding/base64"
	"strconv"
	"testing"
	"time"
)

func TestSessionCodec_RoundTrip(t *testing.T) {
	codec, _ := NewSessionCodec("abcdefghijklmnopqrstuvwxyz012345", time.Hour)
	token, err := codec.Encode(7, 3)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	userID, authVersion, err := codec.Decode(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if userID != 7 || authVersion != 3 {
		t.Fatalf("decode = (%d, %d), want (7, 3)", userID, authVersion)
	}
}

func TestSessionCodec_RejectsTampering(t *testing.T) {
	codec, _ := NewSessionCodec("abcdefghijklmnopqrstuvwxyz012345", time.Hour)
	token, err := codec.Encode(1, 0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, _, err := codec.Decode(token[:len(token)-2] + "xx"); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	other, _ := NewSessionCodec("abcdefghijklmnopqrstuvwxyzABCDEFGH", time.Hour)
	if _, _, err := other.Decode(token); err == nil {
		t.Fatal("wrong secret must be rejected")
	}
}

func TestSessionCodec_RejectsExpired(t *testing.T) {
	codec, _ := NewSessionCodec("abcdefghijklmnopqrstuvwxyz012345", time.Millisecond)
	token, err := codec.Encode(1, 0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, _, err := codec.Decode(token); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

// TestSessionCodec_RejectsLegacySchema pins the upgrade policy: a cookie minted
// under the previous payload layout (uid/name/role/exp, no schema field) is
// rejected outright, so the change forces a re-login instead of being read with
// stale authorization semantics.
func TestSessionCodec_RejectsLegacySchema(t *testing.T) {
	codec, _ := NewSessionCodec("abcdefghijklmnopqrstuvwxyz012345", time.Hour)
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	legacy := []byte(`{"uid":7,"name":"alice","role":"admin","exp":` + exp + `}`)
	body := base64.RawURLEncoding.EncodeToString(legacy)
	token := body + "." + codec.mac(body)
	if _, _, err := codec.Decode(token); err == nil {
		t.Fatal("legacy (pre-schema) cookie must be rejected")
	}
}

func TestSessionCodec_RejectsShortSecret(t *testing.T) {
	_, err := NewSessionCodec("short", time.Hour)
	if err == nil {
		t.Fatal("expected error for short secret, got nil")
	}
	// 32 bytes should succeed
	c, err := NewSessionCodec("abcdefghijklmnopqrstuvwxyz012345", time.Hour)
	if err != nil {
		t.Fatalf("32-byte secret should succeed: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil codec")
	}
}
