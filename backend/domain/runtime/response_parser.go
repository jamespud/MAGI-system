package runtime

import (
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

type ResponseType string

const (
	ResponseToolCall       ResponseType = "tool_call"
	ResponseEvidenceSummary ResponseType = "evidence_summary"
	ResponseVote            ResponseType = "vote"
	ResponseInvalid         ResponseType = "invalid"
)

type ParsedResponse struct {
	Type    ResponseType
	Summary *entity.EvidenceSummary
	Vote    *entity.Vote
	Raw     *schema.Message
}

func parseResponse(
	resp *schema.Message,
	phase string,
	sumVal *validation.TypedValidator[entity.EvidenceSummary],
	voteVal *validation.TypedValidator[entity.Vote],
) *ParsedResponse {
	pr := &ParsedResponse{Raw: resp}
	if len(resp.ToolCalls) > 0 {
		pr.Type = ResponseToolCall
		return pr
	}
	switch phase {
	case "gather", "reconsider_gather":
		summary, vr := sumVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			pr.Type = ResponseEvidenceSummary
			pr.Summary = summary
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
