package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel/attribute"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

// Tracing wraps every request in a span and exposes the trace id via the
// X-Trace-ID response header. A nil provider (tracing disabled) is a no-op.
func Tracing(tp *sdkTrace.TracerProvider) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if tp == nil {
			c.Next(ctx)
			return
		}
		tr := tp.Tracer("magi.server")
		name := "HTTP " + string(c.Method()) + " " + string(c.Request.URI().Path())
		spanCtx, span := tr.Start(ctx, name)
		span.SetAttributes(
			attribute.String("http.method", string(c.Method())),
			attribute.String("http.path", string(c.Request.URI().Path())),
		)
		c.Header("X-Trace-ID", span.SpanContext().TraceID().String())
		c.Next(spanCtx)
		span.SetAttributes(attribute.Int("http.status", c.Response.StatusCode()))
		span.End()
	}
}
