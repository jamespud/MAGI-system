// m1-e2e is an evidence-only entry point for ROLLOUT-LAB-E2E-01.
//
// It can emit EVIDENCE_READY when every configured M1-00 stage supplies real
// evidence, but it can never award PASS_M1. Independent acceptance is a
// separate M1-GATE.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jamespud/magi/backend/internal/m1e2e"
)

var ErrEvidenceNotReady = errors.New("M1 evidence is not ready for independent gate")

func main() {
	if err := runCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrEvidenceNotReady) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func runCLI(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("m1-e2e", flag.ContinueOnError)
	flags.SetOutput(stderr)
	modeValue := flags.String("mode", "real", "evidence mode: real or synthetic")
	strict := flags.Bool("strict", false, "exit 2 unless all M1-00 evidence is ready")
	if err := flags.Parse(args); err != nil {
		return err
	}
	mode, err := parseMode(*modeValue)
	if err != nil {
		return err
	}
	scenario, err := m1e2e.LoadScenario()
	if err != nil {
		return err
	}
	result, err := (m1e2e.Runner{
		Scenario:     scenario,
		Mode:         mode,
		Capabilities: m1e2e.Capabilities{},
	}).Run(context.Background())
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if *strict && result.Status != m1e2e.StatusEvidenceReady {
		return fmt.Errorf("%w: %s", ErrEvidenceNotReady, result)
	}
	return nil
}

func parseMode(value string) (m1e2e.Mode, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case string(m1e2e.ModeReal):
		return m1e2e.ModeReal, nil
	case string(m1e2e.ModeSynthetic):
		return m1e2e.ModeSynthetic, nil
	default:
		return "", fmt.Errorf("invalid -mode %q: want real or synthetic", value)
	}
}
