package runtime

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

type ResponseType string

const (
	ResponseToolCall        ResponseType = "tool_call"
	ResponseClaimSubmission ResponseType = "claim_submission"
	ResponseEvidenceSummary ResponseType = "evidence_summary"
	ResponseReflection      ResponseType = "reflection"
	ResponseVote            ResponseType = "vote"
	ResponseInvalid         ResponseType = "invalid"
)

type ParsedResponse struct {
	Type       ResponseType
	Summary    *entity.EvidenceSummary
	Vote       *entity.Vote
	Claims     *entity.ClaimSubmission
	Reflection *entity.Reflection
	Raw        *schema.Message
}

func parseResponse(
	resp *schema.Message,
	phase string,
	sumVal *validation.TypedValidator[entity.EvidenceSummary],
	voteVal *validation.TypedValidator[entity.Vote],
	claimVal *validation.TypedValidator[entity.ClaimSubmission],
	reflectionVal *validation.TypedValidator[entity.Reflection],
) *ParsedResponse {
	pr := &ParsedResponse{Raw: resp}
	if len(resp.ToolCalls) > 0 {
		pr.Type = ResponseToolCall
		return pr
	}

	// Try discriminator-based routing first.
	var discriminator struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(resp.Content), &discriminator) == nil && discriminator.Type != "" {
		switch discriminator.Type {
		case "claim_submission":
			if phase == "gather" || phase == "reconsider_gather" {
				cs, vr := claimVal.ValidateAndUnmarshal([]byte(resp.Content))
				if vr != nil && vr.Valid {
					pr.Type = ResponseClaimSubmission
					pr.Claims = cs
					return pr
				}
			}
		case "evidence_summary":
			if phase == "gather" || phase == "reconsider_gather" {
				summary, vr := sumVal.ValidateAndUnmarshal([]byte(resp.Content))
				if vr != nil && vr.Valid {
					pr.Type = ResponseEvidenceSummary
					pr.Summary = summary
					return pr
				}
			}
		case "vote":
			if phase == "vote" {
				vote, vr := voteVal.ValidateAndUnmarshal([]byte(resp.Content))
				if vr != nil && vr.Valid {
					pr.Type = ResponseVote
					pr.Vote = vote
					return pr
				}
			}
		}
	}

	// Fallback: phase-based schema try (no discriminator).
	switch phase {
	case "gather", "reconsider_gather":
		summary, vr := sumVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			pr.Type = ResponseEvidenceSummary
			pr.Summary = summary
			return pr
		}
	case "reconsider_reflect":
		reflection, vr := reflectionVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			pr.Type = ResponseReflection
			pr.Reflection = reflection
			return pr
		}
	case "vote":
		vote, vr := voteVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			pr.Type = ResponseVote
			pr.Vote = vote
			return pr
		}
	}

	pr.Type = ResponseInvalid
	return pr
}
