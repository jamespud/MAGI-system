package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/server/dto"
)

// Auth returns middleware that authenticates bearer/API-key tokens. When the
// auth service is disabled the request passes through unchanged (open mode),
// preserving local development behavior.
// publicPaths are exempt from authentication (health, docs, metrics).
var publicPaths = map[string]bool{
	"/health": true, "/ready": true, "/version": true,
	"/openapi.json": true, "/metrics": true,
}

func Auth(authSvc *auth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if authSvc == nil || !authSvc.Enabled() || publicPaths[string(c.Path())] {
			c.Next(ctx)
			return
		}
		token := auth.BearerToken(string(c.GetHeader("Authorization")))
		if token == "" {
			token = string(c.GetHeader("X-API-Key"))
		}
		p, ok := authSvc.Authenticate(ctx, token)
		if !ok {
			c.JSON(consts.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized"})
			c.Abort()
			return
		}
		c.Next(auth.WithPrincipal(ctx, p))
	}
}

// RequireRole gates a route to a specific principal role. Open mode (nil
// principal, auth disabled) passes through for local development.
func RequireRole(role string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		p := auth.PrincipalFrom(ctx)
		if p == nil || p.Role == role {
			c.Next(ctx)
			return
		}
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
		c.Abort()
	}
}
