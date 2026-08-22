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
	return &WebFetchToolExecutor{
		allowed: allowed, maxBytes: maxBytes, timeout: timeout,
		client: &http.Client{Timeout: timeout},
	}, nil
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
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("web_fetch: only http/https urls are allowed")
	}
	if !e.allowed[strings.ToLower(parsed.Hostname())] {
		return nil, fmt.Errorf("web_fetch: host %q is not in the allowed domains", parsed.Hostname())
	}
	if net.ParseIP(parsed.Hostname()) == nil {
		if err := checkDNS(parsed.Hostname()); err != nil {
			return nil, err
		}
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
	out := map[string]any{"url": parsed.String(), "content": text, "truncated": false}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out, SourceURI: parsed.String()}, nil
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

// checkDNS ensures the resolved host is not a private address (SSRF guard).
func checkDNS(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("web_fetch: resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
			return fmt.Errorf("web_fetch: host %q resolves to a private address", host)
		}
	}
	return nil
}

var _ port.ToolExecutorPort = (*WebFetchToolExecutor)(nil)
