package runtime

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

func mustSumVal(t *testing.T) *validation.TypedValidator[entity.EvidenceSummary] {
	t.Helper()
	v, err := validation.NewTypedValidator[entity.EvidenceSummary](validation.NewReflectSchemaGenerator(), validation.NewJSONSchemaValidator())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustVoteVal(t *testing.T) *validation.TypedValidator[entity.Vote] {
	t.Helper()
	v, err := validation.NewTypedValidator[entity.Vote](validation.NewReflectSchemaGenerator(), validation.NewJSONSchemaValidator())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustClaimVal(t *testing.T) *validation.TypedValidator[entity.ClaimSubmission] {
	t.Helper()
	v, err := validation.NewTypedValidator[entity.ClaimSubmission](validation.NewReflectSchemaGenerator(), validation.NewJSONSchemaValidator())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustReflVal(t *testing.T) *validation.TypedValidator[entity.Reflection] {
	t.Helper()
	v, err := validation.NewTypedValidator[entity.Reflection](validation.NewReflectSchemaGenerator(), validation.NewJSONSchemaValidator())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseResponse_StripsCodeFencesAroundSummary(t *testing.T) {
	// DeepSeek often wraps JSON in ```json fences.
	fenced := "```json\n{\"evidence_by_type\":{\"quantitative\":[\"EV-001\"]},\"claims\":[],\"ready\":true}\n```"
	resp := schema.AssistantMessage(fenced, nil)
	pr := parseResponse(resp, "gather", mustSumVal(t), mustVoteVal(t), mustClaimVal(t), mustReflVal(t))
	if pr.Type != ResponseEvidenceSummary {
		t.Fatalf("expected fenced JSON recognized as EvidenceSummary, got %s", pr.Type)
	}
}

func TestParseResponse_StripsCodeFencesAroundVote(t *testing.T) {
	fenced := "```json\n{\"decision\":\"approve\",\"confidence\":80,\"utility_scores\":[{\"dimension_code\":\"correctness\",\"score\":90,\"evidence_ids\":[\"EV-001\"],\"reasoning\":\"r\"}],\"evidence_ids\":[\"EV-001\"]}\n```"
	resp := schema.AssistantMessage(fenced, nil)
	pr := parseResponse(resp, "vote", mustSumVal(t), mustVoteVal(t), mustClaimVal(t), mustReflVal(t))
	if pr.Type != ResponseVote {
		t.Fatalf("expected fenced JSON recognized as Vote, got %s", pr.Type)
	}
}

func TestParseResponse_StripsDiscriminatorTypeField(t *testing.T) {
	// LLMs often emit a "type" discriminator field; with additionalProperties:false
	// the schema rejects it. parseResponse must strip it before validating.
	withType := `{"type":"evidence_summary","evidence_by_type":{"quantitative":["EV-001"]},"claims":[],"ready":true}`
	resp := schema.AssistantMessage(withType, nil)
	pr := parseResponse(resp, "gather", mustSumVal(t), mustVoteVal(t), mustClaimVal(t), mustReflVal(t))
	if pr.Type != ResponseEvidenceSummary {
		t.Fatalf("expected summary with discriminator field recognized, got %s", pr.Type)
	}
}
