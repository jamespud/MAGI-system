package server

import (
	"bytes"
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/metrics"
)

// Metrics returns middleware that counts every request.
func Metrics(reg *metrics.Registry) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		reg.IncRequests()
		c.Next(ctx)
	}
}

// MetricsHandler serves the counters in Prometheus text exposition format.
func MetricsHandler(reg *metrics.Registry) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var buf bytes.Buffer
		reg.WritePrometheus(&buf)
		c.Data(consts.StatusOK, "text/plain; version=0.0.4; charset=utf-8", buf.Bytes())
	}
}
