package service

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

func BuildReport(resolution *entity.Resolution, votes []*entity.Vote, evidence []*entity.EvidenceRecord, claims []*entity.Claim) string {
	var b strings.Builder
	if resolution != nil {
		b.WriteString(resolution.FinalReport)
		b.WriteString("\n\n--- Consensus ---\n")
		b.WriteString(string(resolution.Consensus.Outcome))
		b.WriteString(" (round ")
		fmt.Fprintf(&b, "%d", resolution.Consensus.Round)
		b.WriteString(")\n\n--- Votes ---\n")
	}
	for _, v := range votes {
		fmt.Fprintf(&b, "- %s (confidence %.0f)\n", v.Decision, entity.NormalizeConfidence(v.Confidence))
	}
	if len(evidence) > 0 {
		b.WriteString("\n--- Key Evidence ---\n")
		for i, ev := range evidence {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "- %s: %s (reliability %.2f)\n", ev.ID, ev.Observation, ev.Reliability.Final)
		}
	}
	if len(claims) > 0 {
		b.WriteString("\n--- Key Claims ---\n")
		for i, cl := range claims {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", cl.ID, cl.Statement)
		}
	}
	return b.String()
}

// RenderDissent renders structured minority positions as a markdown section.
func RenderDissent(dissent []entity.Dissent) string {
	if len(dissent) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range dissent {
		fmt.Fprintf(&b, "- %s dissented with %s", d.AgentCode, d.Decision)
		if d.Reasoning != "" {
			fmt.Fprintf(&b, ": %s", d.Reasoning)
		}
		if len(d.EvidenceIDs) > 0 {
			fmt.Fprintf(&b, " (evidence: %v)", d.EvidenceIDs)
		}
		if len(d.Conditions) > 0 {
			var conds []string
			for _, c := range d.Conditions {
				conds = append(conds, c.Statement)
			}
			fmt.Fprintf(&b, " (conditions: %v)", conds)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RenderReport renders a validated FinalReportData into a markdown report.
// Empty list sections are omitted. A nil input returns "".
func RenderReport(d *entity.FinalReportData) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Decision Report\n\n")
	fmt.Fprintf(&b, "## Decision\n%s\n\n", d.Decision)
	fmt.Fprintf(&b, "## Summary\n%s\n", d.Summary)
	if len(d.KeyReasons) > 0 {
		b.WriteString("\n## Key Reasons\n")
		for _, r := range d.KeyReasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if len(d.Risks) > 0 {
		b.WriteString("\n## Risks\n")
		for _, r := range d.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if len(d.NextSteps) > 0 {
		b.WriteString("\n## Next Steps\n")
		for _, s := range d.NextSteps {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(d.KeyEvidenceIDs) > 0 {
		b.WriteString("\n## Key Evidence\n")
		for _, id := range d.KeyEvidenceIDs {
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}
	if len(d.KeyClaimIDs) > 0 {
		b.WriteString("\n## Key Claims\n")
		for _, id := range d.KeyClaimIDs {
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}
	return b.String()
}
