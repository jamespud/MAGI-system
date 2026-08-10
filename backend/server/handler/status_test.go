package handler_test

import (
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/server/handler"
)

func TestStatusHandler_ReportsLiveState(t *testing.T) {
	reg := metrics.New()
	reg.AddTokens(1234)
	reg.AddCostUSD(0.25)
	reg.RunStart()
	h := handler.NewStatusHandler(reg, "deepseek-v4-flash", 12)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/api/v1/status", h.Status)

	w := ut.PerformRequest(r.Engine, "GET", "/api/v1/status", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d", resp.StatusCode())
	}
	body := string(resp.Body())
	for _, want := range []string{`"model_name":"deepseek-v4-flash"`, `"max_steps":12`, `"tokens_total":1234`, `"cost_usd":0.25`, `"runs_active":1`, `"connected":true`} {
		if !contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}
