package server

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

func RequestID() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		rid := fmt.Sprintf("req-%d", time.Now().UnixNano())
		c.Header("X-Request-ID", rid)
		c.Set("request_id", rid)
		c.Next(ctx)
	}
}

func Recovery() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				// Do not echo panic internals to clients: they may contain
				// stack traces or sensitive values. Log server-side instead.
				c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{
					Error: "internal server error",
				})
				c.Abort()
			}
		}()
		c.Next(ctx)
	}
}

// Logger logs each request with RequestID, method, path, status, and duration.
func Logger() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		c.Next(ctx)
		rid := string(c.GetHeader("X-Request-ID"))
		fmt.Printf("[HTTP] %s %s %d %s %s\n",
			string(c.Method()),
			string(c.Request.URI().Path()),
			c.Response.StatusCode(),
			time.Since(start),
			rid,
		)
	}
}

// AuditMiddleware records one audit event per request after the handler runs.
// It is attached to administrative and sensitive routes so the audit trail
// captures who (principal) performed which action and with what result.
func AuditMiddleware(svc *audit.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(ctx)
		if svc == nil {
			return
		}
		method := string(c.Method())
		path := string(c.Request.URI().Path())
		event := &entity.AuditEvent{
			Action:   method,
			Resource: path,
			Status:   c.Response.StatusCode(),
		}
		if p := auth.PrincipalFrom(ctx); p != nil {
			event.UserID = p.UserID
			event.Username = p.Name
			event.Role = p.Role
		}
		// Use a fresh context: the request context may be canceled by the time
		// the response is flushed, and audit writes must not fail requests.
		_ = svc.Record(context.Background(), event)
	}
}
