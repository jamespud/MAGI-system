package tracing_test

import (
	"testing"

	"github.com/jamespud/magi/backend/application/tracing"
)

func TestProvider_OTLPEndpointBuildsProvider(t *testing.T) {
	tp := tracing.NewProvider(tracing.Config{Enabled: true, OTLPEndpoint: "http://127.0.0.1:4318"}, nil)
	if tp == nil {
		t.Fatal("expected provider with OTLP endpoint")
	}
	_ = tp.Shutdown(t.Context())
}
