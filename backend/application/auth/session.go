package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// sessionPayload is the signed session cookie body.
type sessionPayload struct {
	UserID int64  `json:"uid"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
}

// SessionCodec issues and verifies HMAC-signed session cookies. No server-side
// session store is required; the cookie itself is the credential.
type SessionCodec struct {
	secret []byte
	ttl    time.Duration
}

// NewSessionCodec builds a codec from a secret and TTL.
func NewSessionCodec(secret string, ttl time.Duration) (*SessionCodec, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("session: secret must be at least 32 bytes")
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &SessionCodec{secret: []byte(secret), ttl: ttl}, nil
}

// SessionTTL returns the cookie lifetime.
func (c *SessionCodec) SessionTTL() time.Duration {
	return c.ttl
}

// Encode signs a principal into a cookie token with an expiry.
func (c *SessionCodec) Encode(p *Principal) (string, error) {
	if p == nil {
		return "", errors.New("session: principal is required")
	}
	payload := sessionPayload{UserID: p.UserID, Name: p.Name, Role: p.Role, Exp: time.Now().Add(c.ttl).Unix()}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := c.mac(body)
	return body + "." + mac, nil
}

// Decode verifies the signature and expiry, returning the principal.
func (c *SessionCodec) Decode(token string) (*Principal, error) {
	parts := splitToken(token)
	if parts == nil {
		return nil, errors.New("session: malformed token")
	}
	body, mac := parts[0], parts[1]
	if !hmac.Equal([]byte(c.mac(body)), []byte(mac)) {
		return nil, errors.New("session: invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("session: invalid body")
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("session: invalid payload")
	}
	if time.Now().Unix() >= payload.Exp {
		return nil, errors.New("session: expired")
	}
	return &Principal{UserID: payload.UserID, Name: payload.Name, Role: payload.Role}, nil
}

func (c *SessionCodec) mac(body string) string {
	m := hmac.New(sha256.New, c.secret)
	_, _ = m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func splitToken(token string) []string {
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			if i == 0 || i == len(token)-1 {
				return nil
			}
			return []string{token[:i], token[i+1:]}
		}
	}
	return nil
}
