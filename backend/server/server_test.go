package server_test

import (
	"strings"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/server"
)

func TestHealthEndpoint(t *testing.T) {
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	server.RegisterRoutes(h)
	w := ut.PerformRequest(h.Engine, "GET", "/health", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d", resp.StatusCode())
	}
	body := string(resp.Body())
	if !strings.Contains(body, "ok") {
		t.Fatalf("body: %s", body)
	}
}
