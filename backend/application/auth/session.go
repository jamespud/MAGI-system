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

// SessionSchemaVersion identifies the signed-cookie payload layout. A cookie
// minted under a different version is rejected outright, so changing the
// payload shape forces a re-login instead of being read with stale semantics.
const SessionSchemaVersion = 2

// sessionPayload is the signed session cookie body. It deliberately carries
// only stable identity plus the authorization version — never role or status,
// which are authorization facts and are re-read from the user store per
// request (see SessionAuthorizer).
type sessionPayload struct {
	Schema      int   `json:"v"`
	UserID      int64 `json:"uid"`
	AuthVersion int64 `json:"av"`
	Exp         int64 `json:"exp"`
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

// Encode signs a user id + auth version into a cookie token with an expiry.
func (c *SessionCodec) Encode(userID, authVersion int64) (string, error) {
	if userID == 0 {
		return "", errors.New("session: user id is required")
	}
	payload := sessionPayload{
		Schema:      SessionSchemaVersion,
		UserID:      userID,
		AuthVersion: authVersion,
		Exp:         time.Now().Add(c.ttl).Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := c.mac(body)
	return body + "." + mac, nil
}

// Decode verifies the signature, schema version and expiry, returning the
// claimed user id and auth version. It does NOT assert that the session is
// still authorized: the caller must resolve current role/status/version.
func (c *SessionCodec) Decode(token string) (userID int64, authVersion int64, err error) {
	parts := splitToken(token)
	if parts == nil {
		return 0, 0, errors.New("session: malformed token")
	}
	body, mac := parts[0], parts[1]
	if !hmac.Equal([]byte(c.mac(body)), []byte(mac)) {
		return 0, 0, errors.New("session: invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return 0, 0, errors.New("session: invalid body")
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, 0, errors.New("session: invalid payload")
	}
	if payload.Schema != SessionSchemaVersion {
		return 0, 0, errors.New("session: unsupported schema version")
	}
	if time.Now().Unix() >= payload.Exp {
		return 0, 0, errors.New("session: expired")
	}
	if payload.UserID == 0 {
		return 0, 0, errors.New("session: missing user id")
	}
	return payload.UserID, payload.AuthVersion, nil
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
