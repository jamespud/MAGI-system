package server_test

import (
	"strings"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/server"
)

func TestMetricsEndpoint_ExposesCounters(t *testing.T) {
	reg := metrics.New()
	reg.IncCasesCreated()
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Metrics(reg))
	h.GET("/metrics", server.MetricsHandler(reg))

	w := ut.PerformRequest(h.Engine, "GET", "/metrics", nil)
	if w.Code != 200 {
		t.Fatalf("metrics: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "magi_cases_created_total 1") || !strings.Contains(body, "magi_requests_total 1") {
		t.Fatalf("counters missing:\n%s", body)
	}
}
