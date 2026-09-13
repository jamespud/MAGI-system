package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// OIDCConfig configures the optional OpenID Connect authorization-code login.
type OIDCConfig struct {
	Enabled          bool
	Issuer           string
	ClientID         string
	ClientSecret     string
	RedirectURL      string
	Scopes           []string
	SelfRegistration bool
}

// OIDCIdentity is the authenticated identity from the provider.
type OIDCIdentity struct {
	Sub           string
	Email         string
	Name          string
	EmailVerified bool
}

// OIDCUserStore resolves or provisions a local account for an identity.
type OIDCUserStore interface {
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	// FindByOIDCSubject resolves the account bound to the provider subject. The
	// subject is immutable at the provider, so it is the account's stable link.
	FindByOIDCSubject(ctx context.Context, subject string) (*entity.User, error)
	// SetOIDCSubject binds a legacy (email-only) account to a subject once.
	SetOIDCSubject(ctx context.Context, userID int64, subject string) error
	Create(ctx context.Context, u *entity.User) error
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// OIDCClient implements the authorization-code flow against an OIDC issuer
// using only the standard library.
type OIDCClient struct {
	cfg         OIDCConfig
	http        *http.Client
	users       OIDCUserStore
	once        sync.Once
	discovery   *oidcDiscovery
	discoverErr error
}

// NewOIDCClient validates the configuration.
func NewOIDCClient(cfg OIDCConfig, users OIDCUserStore) (*OIDCClient, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("oidc is not enabled")
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.ClientID) == "" ||
		strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, fmt.Errorf("oidc: issuer, client_id and redirect_url are required")
	}
	if users == nil {
		return nil, fmt.Errorf("oidc: user store is required")
	}
	return &OIDCClient{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}, users: users}, nil
}

func (c *OIDCClient) discover(ctx context.Context) (*oidcDiscovery, error) {
	c.once.Do(func() {
		base := strings.TrimRight(c.cfg.Issuer, "/")
		wellKnown := base + "/.well-known/openid-configuration"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
		if err != nil {
			c.discoverErr = err
			return
		}
		resp, err := c.http.Do(req)
		if err != nil {
			c.discoverErr = err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			c.discoverErr = fmt.Errorf("oidc discovery status %d", resp.StatusCode)
			return
		}
		var d oidcDiscovery
		if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
			c.discoverErr = err
			return
		}
		c.discovery = &d
	})
	return c.discovery, c.discoverErr
}

// AuthorizationURL returns the provider authorization URL for a state value.
func (c *OIDCClient) AuthorizationURL(state string) (string, error) {
	d, err := c.discover(context.Background())
	if err != nil || d == nil {
		return "", fmt.Errorf("oidc: discover: %w", err)
	}
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", c.cfg.ClientID)
	params.Set("redirect_uri", c.cfg.RedirectURL)
	params.Set("scope", strings.Join(defaultScopes(c.cfg.Scopes), " "))
	params.Set("state", state)
	return d.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// Exchange trades an authorization code for the identity via token +
// userinfo endpoints.
func (c *OIDCClient) Exchange(ctx context.Context, code string) (*OIDCIdentity, error) {
	d, err := c.discover(ctx)
	if err != nil || d == nil {
		return nil, fmt.Errorf("oidc: discover: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.cfg.RedirectURL)
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("oidc: token exchange status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("oidc: token decode: %w", err)
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("oidc: token response missing access_token")
	}
	userinfo, err := http.NewRequestWithContext(ctx, http.MethodGet, d.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	userinfo.Header.Set("Authorization", "Bearer "+token.AccessToken)
	uiResp, err := c.http.Do(userinfo)
	if err != nil {
		return nil, fmt.Errorf("oidc: userinfo: %w", err)
	}
	defer uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: userinfo status %d", uiResp.StatusCode)
	}
	var identity struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.NewDecoder(uiResp.Body).Decode(&identity); err != nil {
		return nil, fmt.Errorf("oidc: userinfo decode: %w", err)
	}
	if identity.Sub == "" {
		return nil, fmt.Errorf("oidc: userinfo missing sub")
	}
	return &OIDCIdentity{
		Sub: identity.Sub, Email: identity.Email, Name: identity.Name,
		EmailVerified: identity.EmailVerified,
	}, nil
}

// Provision resolves a local account for the identity, auto-provisioning a
// user-role account when self-registration is enabled.
func (c *OIDCClient) Provision(ctx context.Context, identity *OIDCIdentity) (*entity.User, error) {
	if identity == nil || identity.Sub == "" {
		return nil, fmt.Errorf("oidc: identity subject is required")
	}
	if !identity.EmailVerified {
		// Email drives account discovery and provisioning, so an unverified
		// address must never grant access to an existing account.
		return nil, fmt.Errorf("oidc: provider did not verify the account email")
	}

	// 1) The immutable binding wins: a subject match is authoritative even if the
	//    provider's email for that account changed.
	bySubject, err := c.users.FindByOIDCSubject(ctx, identity.Sub)
	switch {
	case err == nil && bySubject != nil:
		return bySubject, nil
	case err == nil, errors.Is(err, port.ErrUserNotFound):
		// continue
	default:
		return nil, fmt.Errorf("oidc: lookup subject %q: %w", identity.Sub, err)
	}

	// 2) Legacy accounts predate the subject column. Adopt them once, but only
	//    when the verified email matches exactly; a different subject claiming
	//    the same email must not inherit the account.
	if identity.Email != "" {
		existing, derr := c.users.FindByEmail(ctx, identity.Email)
		switch {
		case derr == nil && existing != nil:
			if existing.OIDCSubject != "" && existing.OIDCSubject != identity.Sub {
				return nil, fmt.Errorf("oidc: account for %q is already bound to another subject", identity.Email)
			}
			if existing.OIDCSubject == "" {
				if err := c.users.SetOIDCSubject(ctx, existing.ID, identity.Sub); err != nil {
					return nil, fmt.Errorf("oidc: bind subject: %w", err)
				}
				existing.OIDCSubject = identity.Sub
			}
			return existing, nil
		case derr == nil, errors.Is(derr, port.ErrUserNotFound):
			// No local account yet; fall through to provisioning.
		default:
			return nil, fmt.Errorf("oidc: lookup user %q: %w", identity.Email, derr)
		}
	}
	if !c.cfg.SelfRegistration {
		return nil, fmt.Errorf("oidc: no local account for %q and self-registration is disabled", identity.Email)
	}
	name := identity.Name
	if name == "" {
		name = identity.Email
	}
	user := &entity.User{Name: name, Email: identity.Email, Role: entity.RoleUser, OIDCSubject: identity.Sub}
	if err := c.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("oidc: provision user: %w", err)
	}
	return user, nil
}

func defaultScopes(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	return []string{"openid", "email", "profile"}
}
