package tracing_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/jamespud/magi/backend/application/tracing"
)

func TestProvider_EnabledWritesSpanLines(t *testing.T) {
	var buf bytes.Buffer
	tp := tracing.NewProvider(tracing.Config{Enabled: true, ServiceName: "magi"}, &buf)
	if tp == nil {
		t.Fatal("expected provider when enabled")
	}
	tr := tp.Tracer("test")
	_, span := tr.Start(context.Background(), "span-a", trace.WithAttributes(attribute.String("k", "v")))
	span.End()
	tp.Shutdown(context.Background())

	out := buf.String()
	if !strings.Contains(out, `name="span-a"`) || !strings.Contains(out, "trace=") {
		t.Fatalf("missing span line: %s", out)
	}
}

func TestProvider_DisabledIsNoop(t *testing.T) {
	tp := tracing.NewProvider(tracing.Config{Enabled: false}, nil)
	if tp != nil {
		t.Fatal("expected nil provider when disabled")
	}
	ctx, span := tracing.Start(context.Background(), "noop")
	span.End()
	if ctx == nil {
		t.Fatal("nil ctx")
	}
}
