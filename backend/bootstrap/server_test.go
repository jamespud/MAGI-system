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

// TestServerListenAddr guards env-var resolution of the HTTP listen address:
// default :8080, and independent MAGI_HTTP_HOST / MAGI_HTTP_PORT.
func TestServerListenAddr(t *testing.T) {
	cases := []struct {
		name string
		host string
		port string
		want string
	}{
		{"default", "", "", ":8080"},
		{"host only", "127.0.0.1", "", "127.0.0.1:8080"},
		{"port only", "", "9090", ":9090"},
		{"host and port", "0.0.0.0", "9000", "0.0.0.0:9000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAGI_HTTP_HOST", tc.host)
			t.Setenv("MAGI_HTTP_PORT", tc.port)
			if got := serverListenAddr(); got != tc.want {
				t.Fatalf("serverListenAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}
