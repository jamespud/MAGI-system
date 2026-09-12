package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
)

// WebFetchToolName is the built-in restricted URL fetch tool.
const WebFetchToolName = "web_fetch"

const (
	defaultWebFetchMaxBytes = 512 * 1024
	defaultWebFetchTimeout  = 10
	// webFetchMaxRedirects bounds how many 30x hops are followed. Each hop is
	// re-validated (scheme + allowlist) and the dialer re-checks the resolved
	// address, so this only limits chain length.
	webFetchMaxRedirects = 5
)

// webFetchArgsSchema is the JSON Schema for web_fetch arguments.
const webFetchArgsSchema = `{"type":"object","properties":{"url":{"type":"string","format":"uri"}},"required":["url"],"additionalProperties":false}`

// WebFetchToolConfig bounds the URL fetch tool to an allow-listed domain set.
type WebFetchToolConfig struct {
	Enabled        bool
	AllowedDomains []string
	MaxBytes       int64
	TimeoutSeconds int
}

// WebFetchToolExecutor fetches a URL and returns plain text. Only http(s)
// URLs whose host is in the allow-list are permitted; responses are
// size-bounded, HTML is reduced to text, and non-text content is rejected.
type WebFetchToolExecutor struct {
	allowed  map[string]bool
	maxBytes int64
	timeout  time.Duration
	client   *http.Client
}

// NewWebFetchToolExecutor validates the allow-list and builds the client.
func NewWebFetchToolExecutor(cfg WebFetchToolConfig) (port.ToolExecutorPort, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("web_fetch tool is not enabled")
	}
	if len(cfg.AllowedDomains) == 0 {
		return nil, fmt.Errorf("web_fetch: at least one allowed domain is required")
	}
	allowed := make(map[string]bool, len(cfg.AllowedDomains))
	for _, domain := range cfg.AllowedDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain != "" {
			allowed[domain] = true
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("web_fetch: at least one non-empty allowed domain is required")
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultWebFetchMaxBytes
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultWebFetchTimeout * time.Second
	}
	e := &WebFetchToolExecutor{allowed: allowed, maxBytes: maxBytes, timeout: timeout}
	// Every connection is dialed through safeDialContext, which resolves the
	// hostname once and connects only to a validated address. This closes the
	// checkDNS -> Dial DNS-rebinding window and applies to every new connection,
	// including each cross-host redirect hop.
	//
	// The request URL's host is never rewritten to the pinned IP: Go's
	// http.Transport derives TLS SNI and certificate verification from
	// req.URL.Host, not from the dialed address, so pinning the IP does not
	// weaken hostname verification. The port is deliberately not restricted —
	// the SSRF control is the resolved IP, and reachability of a port on an
	// allowlisted, publicly-resolving host is the operator's allowlist decision.
	// Proxy is disabled so an ambient HTTP(S)_PROXY cannot re-route the fetch.
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	e.client = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return safeDialContext(ctx, dialer, network, addr)
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   timeout,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webFetchMaxRedirects {
				return fmt.Errorf("web_fetch: stopped after %d redirects", webFetchMaxRedirects)
			}
			return e.validateTarget(req.URL)
		},
	}
	return e, nil
}

func (e *WebFetchToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("web_fetch: parse args: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil {
		return nil, fmt.Errorf("web_fetch: invalid url: %w", err)
	}
	if err := e.validateTarget(parsed); err != nil {
		return nil, err
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if e.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: new request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.5")
	httpReq.Header.Set("User-Agent", "MAGI-web-fetch/1.0")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web_fetch: http status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/") && !strings.Contains(contentType, "application/json") &&
		!strings.Contains(contentType, "application/xhtml") && !strings.Contains(contentType, "application/xml") {
		return nil, fmt.Errorf("web_fetch: unsupported content type %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, e.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("web_fetch: read body: %w", err)
	}
	if int64(len(body)) > e.maxBytes {
		return nil, fmt.Errorf("web_fetch: response exceeds %d bytes", e.maxBytes)
	}
	text := stripHTML(string(body))
	// Report the URL actually fetched after any redirects, not the request URL.
	finalURL := parsed
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL
	}
	out := map[string]any{"url": finalURL.String(), "content": text, "truncated": false}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out, SourceURI: finalURL.String()}, nil
}

var (
	scriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
)

// stripHTML reduces HTML to readable text: drops scripts, styles, then tags.
func stripHTML(input string) string {
	input = scriptRe.ReplaceAllString(input, "")
	input = styleRe.ReplaceAllString(input, "")
	var b strings.Builder
	inTag := false
	for _, r := range input {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	text := b.String()
	text = strings.NewReplacer("\t", " ", "\r", "\n", "\n\n", "\n").Replace(text)
	return strings.TrimSpace(text)
}

// validateTarget enforces the scheme and allowlist for a URL. It is applied to
// the initial request and to every redirect hop so a 30x response cannot reach
// a host the caller did not allow.
func (e *WebFetchToolExecutor) validateTarget(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("web_fetch: missing url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("web_fetch: only http/https urls are allowed")
	}
	if !e.allowed[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("web_fetch: host %q is not in the allowed domains", u.Hostname())
	}
	return nil
}

// safeDialContext resolves the target host once and connects only to an
// address that passes isForbiddenIP, pinning the connection to the validated
// IP so a DNS rebind between validation and dial cannot redirect it inward.
//
// A literal-IP host is dialed as-is: reachability of a literal IP is an
// explicit allowlist decision by the operator (for example 127.0.0.1 in a
// local/test configuration), and there is no resolution step to rebind.
func safeDialContext(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: bad address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip != nil {
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: resolve %q: %w", host, err)
	}
	var lastErr error
	for _, ip := range ips {
		if isForbiddenIP(ip) {
			lastErr = fmt.Errorf("web_fetch: host %q resolves to a forbidden address %s", host, ip)
			continue
		}
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr != nil {
			lastErr = derr
			continue
		}
		return conn, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("web_fetch: host %q has no usable address", host)
	}
	return nil, lastErr
}

// isForbiddenIP reports whether an address must never be dialed by web_fetch:
// loopback, private, unspecified, link-local, interface-local and multicast
// ranges (which include the common cloud metadata endpoints such as
// 169.254.169.254).
func isForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast()
}

var _ port.ToolExecutorPort = (*WebFetchToolExecutor)(nil)
