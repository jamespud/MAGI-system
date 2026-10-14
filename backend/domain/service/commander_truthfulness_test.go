package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
)

// Issue #13 / CAS-8: the report generator must not be able to cite an artifact
// that is not part of the frozen authoritative set handed to it. The old
// reportCites helper accepted any non-empty list, so a fabricated ID passed.
func TestGenerateReport_RejectsFabricatedCitation(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"forged","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-999"]}`, nil),
		schema.AssistantMessage(`{"summary":"forged","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-999"]}`, nil),
		schema.AssistantMessage(`{"summary":"forged","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-999"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, nil)
	if err == nil {
		t.Fatal("report citing an ID outside the authoritative set must be rejected")
	}
}

func TestGenerateReport_RetriesAfterFabricatedCitation(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"forged","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-999"]}`, nil),
		schema.AssistantMessage(`{"summary":"honest","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	out, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected one retry after a fabricated citation, calls=%d", m.calls)
	}
	if strings.Contains(out, "EV-999") {
		t.Fatalf("fabricated citation leaked into the report:\n%s", out)
	}
}

func TestGenerateReport_RejectsDuplicateCitation(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"dup","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001","EV-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"dup","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001","EV-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"dup","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001","EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, nil)
	if err == nil {
		t.Fatal("a duplicate citation must not be accepted as a distinct reference")
	}
}

// The authoritative decision is bound by the deterministic resolution, never by
// the report model. A model that cannot see the decision still produces the
// correct heading.
func TestGenerateReport_BindsAuthoritativeDecision(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	out, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionReject}, nil, []string{"EV-001"}, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(out, "## Decision\nreject") {
		t.Fatalf("report must render the authoritative decision, got:\n%s", out)
	}
}

// The report model must not be able to author the authoritative decision. The
// structured contract no longer has a decision field, so a model that tries to
// supply one fails schema validation instead of silently overriding the
// resolver.
func TestGenerateReport_RejectsModelSuppliedDecision(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"decision":"reject","summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
		schema.AssistantMessage(`{"decision":"reject","summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
		schema.AssistantMessage(`{"decision":"reject","summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, nil)
	if err == nil {
		t.Fatal("a report that supplies its own decision must be rejected")
	}
}

// Evidence and claim citations are different namespaces: a claim id must not
// be accepted as an evidence citation (and vice versa).
func TestGenerateReport_RejectsCrossTypeCitation(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["CL-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["CL-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["CL-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, []string{"CL-001"})
	if err == nil {
		t.Fatal("a claim id cited as evidence must be rejected")
	}
}

// The same id must not be cited in both the evidence and the claim list.
func TestGenerateReport_RejectsIdInBothCitationLists(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"],"key_claim_ids":["EV-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"],"key_claim_ids":["EV-001"]}`, nil),
		schema.AssistantMessage(`{"summary":"s","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"],"key_claim_ids":["EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"},
		&entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil, []string{"EV-001"}, []string{"EV-001"})
	if err == nil {
		t.Fatal("an id cited in both lists must be rejected")
	}
}
