package magi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

func TestWebFetch_StripHTML_RemovesScriptStyle(t *testing.T) {
	input := `<html><head><style>body { color: red; }</style><script>alert('xss')</script></head><body><p>Hello</p></body></html>`
	result := stripHTML(input)
	if strings.Contains(result, "alert") {
		t.Errorf("script content not stripped: %q", result)
	}
	if strings.Contains(result, "color") {
		t.Errorf("style content not stripped: %q", result)
	}
	if !strings.Contains(result, "Hello") {
		t.Errorf("body content missing: %q", result)
	}
}

func TestWebFetch_SafeDialContextRefusesLoopbackHostname(t *testing.T) {
	// localhost resolves to 127.0.0.1/::1, so a hostname rebinding to loopback
	// must be refused at dial time even though no allowlist check happens here.
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	_, err := safeDialContext(context.Background(), dialer, "tcp", "localhost:9")
	if err == nil {
		t.Fatal("expected loopback hostname to be refused")
	}
	if !strings.Contains(err.Error(), "forbidden address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebFetch_IsForbiddenIP(t *testing.T) {
	forbidden := []string{
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "0.0.0.0", "::1", "fe80::1", "fc00::1",
		// IPv4-mapped IPv6 forms of the same ranges must not slip through.
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254",
	}
	for _, s := range forbidden {
		if !isForbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be forbidden", s)
		}
	}
	if isForbiddenIP(net.ParseIP("93.184.216.34")) {
		t.Error("public address should not be forbidden")
	}
	if !isForbiddenIP(nil) {
		t.Error("nil address should be forbidden")
	}
}

// TestWebFetch_HTTPSThroughPinnedDialer proves the pinned dialer does not break
// TLS: the handshake runs with full certificate verification (root pool pinned
// to the test server, no InsecureSkipVerify) and the request Host the server
// observes is the URL host, never a rewritten ip:port.
//
// The URL host here is a literal IP, for which Go omits SNI (RFC 6066), so
// ServerName is expected to be empty. For hostnames the stdlib derives SNI and
// certificate verification from req.URL.Host — which safeDialContext never
// mutates; it only pins the dialed address.
func TestWebFetch_HTTPSVerifiesHostname(t *testing.T) {
	sni := make(chan string, 1)
	hostHeader := make(chan string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sni <- r.TLS.ServerName
		hostHeader <- r.Host
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("secure"))
	}))
	defer srv.Close()

	exec, err := NewWebFetchToolExecutor(WebFetchToolConfig{
		Enabled: true, AllowedDomains: []string{"127.0.0.1"}, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	e, ok := exec.(*WebFetchToolExecutor)
	if !ok {
		t.Fatalf("unexpected executor type %T", exec)
	}
	tr, ok := e.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport %T", e.client.Transport)
	}
	// Trust the test server's self-signed cert so the real verification path
	// (safeDialContext -> TLS handshake) runs without disabling verification.
	clone := tr.Clone()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	clone.TLSClientConfig = &tls.Config{RootCAs: pool}
	e.client.Transport = clone

	res, err := e.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: WebFetchToolName, ArgumentsJSON: `{"url":"` + srv.URL + `/x"}`,
	})
	if err != nil {
		t.Fatalf("https fetch: %v", err)
	}
	if !strings.Contains(res.Output, "secure") {
		t.Fatalf("output = %s", res.Output)
	}
	select {
	case got := <-sni:
		if got != "" {
			t.Fatalf("TLS SNI = %q, want empty for an IP-literal host (RFC 6066)", got)
		}
	default:
		t.Fatal("server did not observe a TLS connection")
	}
	select {
	case got := <-hostHeader:
		if !strings.HasPrefix(got, "127.0.0.1:") {
			t.Fatalf("Host header = %q, want the request host", got)
		}
	default:
		t.Fatal("server did not observe a request")
	}
}
