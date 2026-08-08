package handler_test

import (
	"context"
	"errors"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/server/handler"
)

func TestReady_ChecksDatabasePinger(t *testing.T) {
	down := handler.NewHealthHandler(func(ctx context.Context) error { return errors.New("db down") })
	up := handler.NewHealthHandler(func(ctx context.Context) error { return nil })

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/ready-down", down.Ready)
	r.GET("/ready-up", up.Ready)

	if w := ut.PerformRequest(r.Engine, "GET", "/ready-down", nil); w.Code != 503 {
		t.Fatalf("down: expected 503, got %d", w.Code)
	}
	if w := ut.PerformRequest(r.Engine, "GET", "/ready-up", nil); w.Code != 200 {
		t.Fatalf("up: expected 200, got %d", w.Code)
	}
}
