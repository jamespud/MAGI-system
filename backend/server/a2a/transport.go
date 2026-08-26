// Package a2atransport builds the A2A Agent Card and mounts the official
// HTTP+JSON REST transport inside the Hertz process. MAGI owns task execution;
// this package only exposes discovery and protocol routing.
package a2atransport

import (
	"context"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"

	"github.com/jamespud/magi/backend/server/a2aerror"
)

// WellKnownAgentCardPath is the A2A discovery path.
const WellKnownAgentCardPath = a2asrv.WellKnownAgentCardPath

// MountDeps carries everything the transport needs to register routes.
type MountDeps struct {
	Handler     a2asrv.RequestHandler
	PublicURL   string
	BasePath    string
	Name        string
	Description string
	// MaxRequestBytes caps the complete decoded request body before the SDK
	// JSON parser runs (default 98304 when unset).
	MaxRequestBytes int64
	// Middlewares are applied only to the A2A REST routes (e.g. a distinct
	// rate-limit bucket) so long-lived streams never consume /api/v1 quota.
	Middlewares []app.HandlerFunc
}

// AgentCard builds the static public AgentCard. It contains no secrets, model
// names, internal hosts, or tenant identifiers.
func AgentCard(publicURL, basePath, name, description string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:               name,
		Version:            "2.0.0",
		Description:        description,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/markdown", "application/json"},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(strings.TrimRight(publicURL, "/")+basePath, a2a.TransportProtocolHTTPJSON),
		},
		Capabilities: a2a.AgentCapabilities{Streaming: true, PushNotifications: false},
		Skills: []a2a.AgentSkill{
			{
				ID:          "evidence-driven-decision",
				Name:        "Evidence-driven decision",
				Description: "Researches evidence, reaches consensus, and produces a decision report with a structured result.",
				Tags:        []string{"decision", "research"},
			},
		},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"bearer": a2a.HTTPAuthSecurityScheme{Scheme: "bearer"},
			"apiKey": a2a.APIKeySecurityScheme{Location: a2a.APIKeySecuritySchemeLocationHeader, Name: "X-API-Key"},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			a2a.SecurityRequirements{"bearer": {}},
			a2a.SecurityRequirements{"apiKey": {}},
		},
	}
}

// Mount registers the discovery card and the official REST transport. The
// REST mux is stripped of BasePath so SDK paths like /message:send remain
// intact; the Go module major version (/v2) never appears in protocol URLs.
func Mount(h *hzserver.Hertz, deps MountDeps) {
	card := AgentCard(deps.PublicURL, deps.BasePath, deps.Name, deps.Description)
	cardHandler := a2asrv.NewStaticAgentCardHandler(card)
	h.GET(WellKnownAgentCardPath, adaptor.HertzHandler(cardHandler))

	restHandler := a2asrv.NewRESTHandler(deps.Handler)
	restHandler = normalizeListTasksQuery(restHandler)
	if deps.MaxRequestBytes > 0 {
		restHandler = http.MaxBytesHandler(restHandler, deps.MaxRequestBytes)
	}
	rest := http.StripPrefix(deps.BasePath, restHandler)
	handlers := append([]app.HandlerFunc{}, deps.Middlewares...)
	if deps.MaxRequestBytes > 0 {
		// Hertz buffers the body before the net/http adapter runs, so the
		// MaxBytesReader 413 override cannot fire. Reject known oversized
		// Content-Length bodies here and keep MaxBytesHandler for streaming
		// reads without a declared length.
		handlers = append(handlers, requestBodyLimit(deps.MaxRequestBytes))
	}
	handlers = append(handlers, adaptor.HertzHandler(rest))
	h.Any(deps.BasePath, handlers...)
	h.Any(deps.BasePath+"/*a2a", handlers...)
}

// normalizeListTasksQuery accepts the A2A v2.5.0 official REST client, which
// sends the compatibility spelling `lastUpdatedAfter`, and copies it onto the
// canonical `statusTimestampAfter` before the SDK handler parses it. When both
// are present, the canonical value wins (it is copied only when absent).
func normalizeListTasksQuery(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			inner.ServeHTTP(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("statusTimestampAfter") == "" && q.Get("lastUpdatedAfter") != "" {
			// Clone the request/URL so we never mutate the caller's request.
			req := r.Clone(r.Context())
			urlCopy := *r.URL
			query := urlCopy.Query()
			query.Set("statusTimestampAfter", query.Get("lastUpdatedAfter"))
			urlCopy.RawQuery = query.Encode()
			req.URL = &urlCopy
			inner.ServeHTTP(w, req)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func requestBodyLimit(limit int64) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if int64(c.Request.Header.ContentLength()) > limit {
			a2aerror.Write(c, http.StatusRequestEntityTooLarge, "INVALID_ARGUMENT", "INVALID_ARGUMENT", "request body too large")
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}
