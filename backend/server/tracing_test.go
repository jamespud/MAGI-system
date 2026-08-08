package server_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/server"
)

func TestTracingMiddleware_SetsTraceHeader(t *testing.T) {
	var buf bytes.Buffer
	tp := tracing.NewProvider(tracing.Config{Enabled: true}, &buf)
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Tracing(tp))
	h.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, "GET", "/ping", nil)
	traceID := string(w.Header().Get("X-Trace-ID"))
	if traceID == "" {
		t.Fatal("missing X-Trace-ID header")
	}
	out := buf.String()
	if !strings.Contains(out, `name="HTTP GET /ping"`) || !strings.Contains(out, traceID) {
		t.Fatalf("span missing: %s", out)
	}
	tp.Shutdown(context.Background())
}
