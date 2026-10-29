// m1-provider-capability performs read-only capability discovery for M1-03A.
// It never sends material, issues a delivery receipt, or enables production.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/providercapability"
)

var ErrBlocked = errors.New("provider capability is blocked")

func main() {
	if err := runCLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrBlocked) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("m1-provider-capability", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baseURL := flags.String("base-url", "", "Provider base URL (or ANTHROPIC_BASE_URL)")
	providerID := flags.String("provider-id", "", "stable provider identifier")
	modelID := flags.String("model", "", "explicit model ID from the discovered list")
	output := flags.String("output", "", "optional report path")
	strict := flags.Bool("strict", false, "exit 2 unless every required capability is VERIFIED")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: m1-provider-capability -provider-id ID [-model MODEL] [-base-url URL] [-output FILE] [-strict] (token: ANTHROPIC_AUTH_TOKEN)")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	base := strings.TrimSpace(*baseURL)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	}
	token := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if base == "" {
		return errors.New("base URL is required (set -base-url or ANTHROPIC_BASE_URL)")
	}
	if token == "" {
		return errors.New("auth token is required: set ANTHROPIC_AUTH_TOKEN")
	}
	if strings.TrimSpace(*providerID) == "" {
		return errors.New("provider ID is required")
	}
	provider, err := parseProvider(strings.TrimSpace(*providerID), base)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	models := probeModels(ctx, provider, token, client, time.Now().UTC())
	report := providercapability.Assess(providercapability.Observation{
		Provider:        provider,
		Models:          models,
		SelectedModelID: strings.TrimSpace(*modelID),
		AssessedAtUTC:   time.Now().UTC().Format(time.RFC3339),
	})
	report.Issue = "44"
	report.Scope = "M1-03A"
	if report.Verdict == providercapability.VerdictCapability {
		report.Verdict = providercapability.VerdictBlocked
		report.Blockers = append(report.Blockers, "INDEPENDENT_REVIEW_NOT_PERFORMED")
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	encoded = append(encoded, '\n')
	if _, err := stdout.Write(encoded); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if strings.TrimSpace(*output) != "" {
		if err := os.WriteFile(*output, encoded, 0o600); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	if *strict && report.Verdict != providercapability.VerdictCapability {
		return fmt.Errorf("%w: %s", ErrBlocked, strings.Join(report.Blockers, ", "))
	}
	return nil
}

func parseProvider(id, rawURL string) (providercapability.Provider, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return providercapability.Provider{}, fmt.Errorf("parse base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return providercapability.Provider{}, errors.New("base URL must be an absolute http(s) URL")
	}
	if parsed.User != nil {
		return providercapability.Provider{}, errors.New("base URL must not contain userinfo")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return providercapability.Provider{ID: id, BaseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

func probeModels(ctx context.Context, provider providercapability.Provider, token string, client *http.Client, now time.Time) providercapability.ModelsObservation {
	observation := providercapability.ModelsObservation{
		Outcome:       providercapability.DiscoveryNotRun,
		ObservedAtUTC: now.UTC().Format(time.RFC3339),
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(provider.BaseURL, "/")+"/v1/models", nil)
	if err != nil {
		observation.Outcome = providercapability.DiscoveryFailed
		observation.ErrorCode = "MODEL_DISCOVERY_REQUEST_INVALID"
		return observation
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("anthropic-version", "2023-06-01")
	response, err := client.Do(request)
	if err != nil {
		observation.Outcome = providercapability.DiscoveryFailed
		observation.ErrorCode = "MODEL_DISCOVERY_TRANSPORT_ERROR"
		return observation
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		observation.Outcome = providercapability.DiscoveryFailed
		observation.HTTPStatus = response.StatusCode
		observation.ErrorCode = "MODEL_DISCOVERY_READ_ERROR"
		return observation
	}
	sum := sha256.Sum256(body)
	observation.HTTPStatus = response.StatusCode
	observation.BodySHA256 = hex.EncodeToString(sum[:])
	observation.BodyBytes = len(body)
	observation.ResponseID = response.Header.Get("x-request-id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		observation.Outcome = providercapability.DiscoveryFailed
		observation.ErrorCode = "MODEL_DISCOVERY_HTTP_ERROR"
		return observation
	}
	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		observation.Outcome = providercapability.DiscoveryFailed
		observation.ErrorCode = "MODEL_DISCOVERY_RESPONSE_INVALID"
		return observation
	}
	for _, model := range decoded.Data {
		if strings.TrimSpace(model.ID) != "" {
			observation.ModelIDs = append(observation.ModelIDs, model.ID)
		}
	}
	observation.BodySummary = fmt.Sprintf(`{"data_count":%d}`, len(decoded.Data))
	observation.Outcome = providercapability.DiscoverySucceeded
	return observation
}
