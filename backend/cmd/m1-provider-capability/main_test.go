package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/providercapability"
)

func TestRunCLIEmptyModelsProducesBlockedReportWithoutToken(t *testing.T) {
	const token = "super-secret-test-token"
	t.Setenv("ANTHROPIC_AUTH_TOKEN", token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"-base-url", server.URL,
		"-provider-id", "isolated-gateway",
		"-output", output,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Contains(stdout.String(), token) {
		t.Fatal("stdout leaked auth token")
	}
	var report providercapability.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if report.Verdict != providercapability.VerdictBlocked || report.M1Gate != providercapability.M1GateNotRun {
		t.Fatalf("report = %+v, want BLOCKED / NOT_RUN", report)
	}
	if len(report.Blockers) != 1 || report.Blockers[0] != "NO_VERIFIABLE_MODEL_TARGET" {
		t.Fatalf("blockers = %v", report.Blockers)
	}
	if len(report.Capabilities) != len(providercapability.AllCapabilities) {
		t.Fatalf("capabilities = %d", len(report.Capabilities))
	}
	if report.ApplyVerifiedEmitted || report.DRCPIssued || report.ProductionExposureEnabled {
		t.Fatalf("unsafe report flags = %+v", report)
	}
	if report.Models.BodySHA256 != "fd71b4438dea68046585a59a5e30b570dcf2dc0bf70fbaff48cbd9049f377504" {
		t.Fatalf("body digest = %q", report.Models.BodySHA256)
	}
	fileBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output report: %v", err)
	}
	if !bytes.Equal(fileBytes, stdout.Bytes()) {
		t.Fatal("output file and stdout differ")
	}
}

func TestRunCLIReportsDiscoveryFailureWithoutCallingModel(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"-base-url", server.URL,
		"-provider-id", "isolated-gateway",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	var report providercapability.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Models.Outcome != providercapability.DiscoveryFailed || report.Models.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("models = %+v", report.Models)
	}
	if report.Verdict != providercapability.VerdictBlocked {
		t.Fatalf("verdict = %q", report.Verdict)
	}
}

func TestRunCLIRequiresExplicitTokenSource(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"-base-url", "http://127.0.0.1:15721",
		"-provider-id", "isolated-gateway",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "auth token") {
		t.Fatalf("error = %v, want missing auth token", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no report on invalid invocation", stdout.String())
	}
}

func TestRunCLINoSmokeNeverCallsMessagesWhenTargetDiscovered(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("x-request-id", "models-request")
			_, _ = w.Write([]byte(`{"data":[{"id":"isolated-model"}]}`))
		case "/v1/messages":
			t.Errorf("discovery-only probe called /v1/messages")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runCLI(context.Background(), []string{
		"-base-url", server.URL,
		"-provider-id", "isolated-gateway",
		"-model", "isolated-model",
		"-strict",
	}, &stdout, &stderr)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("strict error = %v, want ErrBlocked", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("strict mode did not emit the report before returning")
	}
	var report providercapability.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Target == nil || report.Target.ModelID != "isolated-model" {
		t.Fatalf("target = %+v", report.Target)
	}
	if report.Verdict != providercapability.VerdictBlocked {
		t.Fatalf("verdict = %q", report.Verdict)
	}
	for _, capability := range report.Capabilities {
		if capability.Status == providercapability.StatusVerified {
			t.Fatalf("%s was verified without independent original evidence", capability.Name)
		}
	}
}

func TestProbeUsesUTCAndDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer server.Close()

	observed := time.Date(2026, 10, 29, 0, 20, 0, 0, time.UTC)
	report := probeModels(context.Background(), providercapability.Provider{
		ID: "provider-a", BaseURL: server.URL,
	}, "token", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.Header.Get("Authorization"), "token") {
			t.Fatal("Authorization header was not applied")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}, observed)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "token") {
		t.Fatal("serialized discovery report leaked token")
	}
	if report.ObservedAtUTC != "2026-10-29T00:20:00Z" {
		t.Fatalf("observed at = %q", report.ObservedAtUTC)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
