package tracing

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config controls distributed tracing.
type Config struct {
	Enabled     bool
	ServiceName string
}

// LoggingSpanProcessor writes finished spans as structured log lines. It is a
// zero-dependency sink suitable for standalone deployments; swap in an OTLP
// exporter later by replacing the processor.
type LoggingSpanProcessor struct {
	mu sync.Mutex
	w  io.Writer
}

func NewLoggingSpanProcessor(w io.Writer) *LoggingSpanProcessor {
	if w == nil {
		w = os.Stdout
	}
	return &LoggingSpanProcessor{w: w}
}

func (p *LoggingSpanProcessor) OnStart(ctx context.Context, s sdkTrace.ReadWriteSpan) {}
func (p *LoggingSpanProcessor) OnEnd(s sdkTrace.ReadOnlySpan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "span name=%q kind=%s trace=%s span=%s parent=%s duration=%s\n",
		s.Name(), s.SpanKind(), s.SpanContext().TraceID(), s.SpanContext().SpanID(),
		s.Parent().SpanID(), s.EndTime().Sub(s.StartTime()))
}
func (p *LoggingSpanProcessor) Shutdown(ctx context.Context) error   { return nil }
func (p *LoggingSpanProcessor) ForceFlush(ctx context.Context) error { return nil }

// NewProvider builds a tracer provider. When disabled it returns nil and the
// global tracer stays no-op, so callers can use tracing.Start safely.
func NewProvider(cfg Config, w io.Writer) *sdkTrace.TracerProvider {
	if !cfg.Enabled {
		return nil
	}
	tp := sdkTrace.NewTracerProvider(
		sdkTrace.WithSpanProcessor(NewLoggingSpanProcessor(w)),
		sdkTrace.WithSampler(sdkTrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	return tp
}

// Start starts a span from the global tracer (no-op when tracing is disabled).
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tr := otel.Tracer("magi")
	return tr.Start(ctx, name, trace.WithAttributes(attrs...))
}
