// Package a2atransport builds the A2A Agent Card and mounts the official
// HTTP+JSON REST transport inside the Hertz process. MAGI owns task execution;
// this package only exposes discovery and protocol routing.
package a2atransport

import (
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
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
	// Middlewares are applied only to the A2A REST routes (e.g. a distinct
	// rate-limit bucket) so long-lived streams never consume /api/v1 quota.
	Middlewares []app.HandlerFunc
}

// AgentCard builds the static public AgentCard. It contains no secrets, model
// names, internal hosts, or tenant identifiers.
func AgentCard(publicURL, basePath, name, description string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:               name,
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

	rest := http.StripPrefix(deps.BasePath, a2asrv.NewRESTHandler(deps.Handler))
	handlers := append([]app.HandlerFunc{}, deps.Middlewares...)
	handlers = append(handlers, adaptor.HertzHandler(rest))
	h.Any(deps.BasePath, handlers...)
	h.Any(deps.BasePath+"/*a2a", handlers...)
}
