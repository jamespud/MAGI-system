package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

const sessionCookieName = "magi_session"

const (
	oidcStateTTL      = 10 * time.Minute
	oidcStatePruneGap = time.Minute
	// oidcStateMaxEntries caps pending authorization requests so the public
	// login endpoint cannot grow the map without bound.
	oidcStateMaxEntries = 10000
)

// oidcStateStore keeps one-time authorization states in memory (single
// instance; multi-replica deployments should back this with shared state).
type oidcStateStore struct {
	mu        sync.Mutex
	states    map[string]time.Time
	lastPrune time.Time
}

func (s *oidcStateStore) issue() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = map[string]time.Time{}
	}
	s.pruneLocked(now)
	if len(s.states) >= oidcStateMaxEntries {
		return "", errors.New("oidc: too many pending authorization requests")
	}
	s.states[state] = now.Add(oidcStateTTL)
	return state, nil
}

// pruneLocked drops expired states. It runs at most once per oidcStatePruneGap
// so the sweep cost is bounded even under a flood of login requests.
func (s *oidcStateStore) pruneLocked(now time.Time) {
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < oidcStatePruneGap {
		return
	}
	s.lastPrune = now
	for k, exp := range s.states {
		if !now.Before(exp) {
			delete(s.states, k)
		}
	}
}

func (s *oidcStateStore) consume(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.states[state]
	if !ok {
		return false
	}
	delete(s.states, state)
	return time.Now().Before(exp)
}

// OIDCHandler wires the OIDC login/callback flow and public self-registration.
type OIDCHandler struct {
	client  *auth.OIDCClient
	session *auth.SessionCodec
	users   *users.Service
	audit   *audit.Service
	states  *oidcStateStore
}

// NewOIDCHandler builds the SSO handler. client may be nil when OIDC is
// disabled (routes are not registered).
func NewOIDCHandler(client *auth.OIDCClient, session *auth.SessionCodec, usersSvc *users.Service, auditSvc *audit.Service) *OIDCHandler {
	return &OIDCHandler{client: client, session: session, users: usersSvc, audit: auditSvc, states: &oidcStateStore{}}
}

func (h *OIDCHandler) Login(ctx context.Context, c *app.RequestContext) {
	if h.client == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "oidc not enabled"})
		return
	}
	state, err := h.states.issue()
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	target, err := h.client.AuthorizationURL(state)
	if err != nil {
		c.JSON(consts.StatusBadGateway, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.Redirect(consts.StatusFound, []byte(target))
}

func (h *OIDCHandler) Callback(ctx context.Context, c *app.RequestContext) {
	if h.client == nil || h.session == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "oidc not enabled"})
		return
	}
	state := string(c.Query("state"))
	code := string(c.Query("code"))
	if state == "" || code == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "state and code are required"})
		return
	}
	if !h.states.consume(state) {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid or expired state"})
		return
	}
	identity, err := h.client.Exchange(ctx, code)
	if err != nil {
		c.JSON(consts.StatusBadGateway, dto.ErrorResponse{Error: err.Error()})
		return
	}
	user, err := h.client.Provision(ctx, identity)
	if err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	token, err := h.session.Encode(&auth.Principal{UserID: user.ID, Name: user.Name, Role: user.Role})
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.SetCookie(sessionCookieName, token, int(h.sessionTTL().Seconds()), "/", "", protocol.CookieSameSiteLaxMode, true, true)
	if h.audit != nil {
		_ = h.audit.Record(ctx, &entity.AuditEvent{Action: "login", Resource: "oidc", Username: user.Name, UserID: user.ID, Role: user.Role, Status: consts.StatusFound})
	}
	c.Redirect(consts.StatusFound, []byte("/"))
}

func (h *OIDCHandler) sessionTTL() time.Duration {
	if h.session == nil {
		return 12 * time.Hour
	}
	return h.session.SessionTTL()
}

// Register creates a self-registered user and returns the one-time key.
func (h *OIDCHandler) Register(ctx context.Context, c *app.RequestContext) {
	if h.users == nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: "users service not configured"})
		return
	}
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
	if err := c.BindAndValidate(&req); err != nil || req.Name == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "name is required"})
		return
	}
	u, key, err := h.users.SelfRegister(ctx, req.Name, req.Email)
	if err != nil {
		if err == users.ErrForbidden {
			c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "self-registration is disabled"})
			return
		}
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	var issued *dto.IssuedKeyDTO
	if key != nil {
		issued = &dto.IssuedKeyDTO{ID: key.ID, Prefix: key.Prefix, Plaintext: key.Plaintext}
	}
	if h.audit != nil {
		_ = h.audit.Record(ctx, &entity.AuditEvent{Action: "register", Resource: "self", Username: u.Name, UserID: u.ID, Role: u.Role, Status: consts.StatusCreated})
	}
	c.JSON(consts.StatusCreated, dto.CreateUserResponse{User: dto.FromUser(u, 0), ApiKey: issued})
}
