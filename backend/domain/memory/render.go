package memory

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

// RenderDocument renders a CaseMemoryProjection into a full long-text document
// for RAG indexing. Unlike BuildProjection (which truncates for the
// case_memory_projection DB table), RenderDocument preserves full content so a
// single case can fill the 1800-token hierarchy level. "What to remember" is a
// domain policy; "how to index" is an adapter concern (ADR-006).
func RenderDocument(proj *entity.CaseMemoryProjection) string {
	if proj == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", proj.QuestionSummary)
	fmt.Fprintf(&b, "Context: %s\n", proj.ContextSummary)
	if len(proj.KeyEvidence) > 0 {
		b.WriteString("Evidence:\n")
		for _, ev := range proj.KeyEvidence {
			fmt.Fprintf(&b, "- [%s] (reliability %.2f) %s\n", ev.EvidenceID, ev.Reliability, ev.Observation)
		}
	}
	if len(proj.KeyClaims) > 0 {
		b.WriteString("Claims:\n")
		for _, cl := range proj.KeyClaims {
			fmt.Fprintf(&b, "- [%s] %s\n", cl.ClaimID, cl.Statement)
		}
	}
	if len(proj.Votes) > 0 {
		b.WriteString("Votes:\n")
		for _, v := range proj.Votes {
			fmt.Fprintf(&b, "- %s: %s (confidence %.2f)\n", v.MagiCode, v.Decision, v.Confidence)
		}
	}
	if len(proj.Tags) > 0 {
		b.WriteString("Tags:\n")
		for _, tag := range proj.Tags {
			fmt.Fprintf(&b, "- %s\n", tag)
		}
	}
	if proj.Annotation != "" {
		fmt.Fprintf(&b, "Annotation: %s\n", proj.Annotation)
	}
	if proj.Resolution != "" {
		fmt.Fprintf(&b, "Resolution: %s\n", proj.Resolution)
	}
	if proj.Outcome != nil {
		fmt.Fprintf(&b, "Outcome: status=%s learned=%s\n", proj.Outcome.Status, proj.Outcome.Learned)
	}
	return b.String()
}
