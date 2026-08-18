package auth

import (
	"testing"
	"time"
)

func TestSessionCodec_RoundTrip(t *testing.T) {
	codec := NewSessionCodec("test-secret", time.Hour)
	token, err := codec.Encode(&Principal{UserID: 7, Name: "alice", Role: "user"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	p, err := codec.Decode(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.UserID != 7 || p.Name != "alice" || p.Role != "user" {
		t.Fatalf("principal = %+v", p)
	}
}

func TestSessionCodec_RejectsTampering(t *testing.T) {
	codec := NewSessionCodec("test-secret", time.Hour)
	token, err := codec.Encode(&Principal{UserID: 1, Name: "a", Role: "admin"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := codec.Decode(token[:len(token)-2] + "xx"); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	other := NewSessionCodec("other-secret", time.Hour)
	if _, err := other.Decode(token); err == nil {
		t.Fatal("wrong secret must be rejected")
	}
}

func TestSessionCodec_RejectsExpired(t *testing.T) {
	codec := NewSessionCodec("test-secret", time.Millisecond)
	token, err := codec.Encode(&Principal{UserID: 1, Name: "a", Role: "user"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := codec.Decode(token); err == nil {
		t.Fatal("expired token must be rejected")
	}
}
