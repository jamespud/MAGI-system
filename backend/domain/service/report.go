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
		fmt.Fprintf(&b, "- %s (confidence %.0f)\n", v.Decision, v.Confidence)
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
