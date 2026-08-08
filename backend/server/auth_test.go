package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/server"
)

func whoamiRoute(h *hzserver.Hertz) {
	h.GET("/whoami", func(ctx context.Context, c *app.RequestContext) {
		p := auth.PrincipalFrom(ctx)
		if p == nil {
			c.JSON(401, map[string]any{"user": 0})
			return
		}
		c.JSON(200, map[string]any{"user": p.UserID})
	})
}

func TestAuthMiddleware_EnforcesToken(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "admin"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	whoamiRoute(h)

	w := ut.PerformRequest(h.Engine, "GET", "/whoami", nil)
	if w.Code != 401 {
		t.Fatalf("missing token: expected 401, got %d", w.Code)
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "Authorization", Value: "Bearer tok-1"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"user":7`) {
		t.Fatalf("valid token: code=%d body=%s", w.Code, w.Body.String())
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "Authorization", Value: "Bearer wrong"})
	if w.Code != 401 {
		t.Fatalf("wrong token: expected 401, got %d", w.Code)
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "X-API-Key", Value: "tok-1"})
	if w.Code != 200 {
		t.Fatalf("X-API-Key header: expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_DisabledAllowsOpen(t *testing.T) {
	svc := auth.NewService(false, nil)
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/open", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})

	w := ut.PerformRequest(h.Engine, "GET", "/open", nil)
	if w.Code != 200 {
		t.Fatalf("disabled auth: expected 200, got %d", w.Code)
	}
}
