package bootstrap

import (
	"context"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/fx"
)

// TestProvideServerEnablesClientDisconnection guards the Hertz option that
// cancels a request context when the peer disconnects. Without it, an A2A SSE
// stream slot and its broker subscription would only be released by heartbeat
// expiry or process shutdown, not promptly on a dropped client socket.
func TestProvideServerEnablesClientDisconnection(t *testing.T) {
	var got *hzserver.Hertz
	app := fx.New(
		fx.NopLogger,
		fx.Provide(provideServer),
		fx.Invoke(func(h *hzserver.Hertz) { got = h }),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx app err: %v", err)
	}
	if got == nil {
		t.Fatal("provideServer returned nil Hertz")
	}
	if !got.GetOptions().SenseClientDisconnection {
		t.Fatal("Hertz client disconnection sensing is disabled")
	}
	_ = app.Stop(context.Background())
}
