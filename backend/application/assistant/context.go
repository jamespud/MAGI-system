package assistant

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

// BuildFollowUpBackground folds the most recent conversation turns and their
// linked resolutions into a deterministic case background.
func BuildFollowUpBackground(messages []*entity.ConversationMessage, resolutions map[string]*entity.Resolution, extra string) string {
	ordered := append([]*entity.ConversationMessage(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	if len(ordered) > 20 {
		ordered = ordered[len(ordered)-20:]
	}

	var history strings.Builder
	wrote := false
	for _, m := range ordered {
		switch m.Role {
		case entity.ConversationRoleUser:
			history.WriteString("User: " + m.Content + "\n")
			wrote = true
		case entity.ConversationRoleAssistant:
			if m.CaseID == "" || resolutions[m.CaseID] == nil {
				continue
			}
			res := resolutions[m.CaseID]
			fmt.Fprintf(&history, "Previous decision (%s): %s (consensus=%s, round=%d)\n",
				m.CaseID, res.FinalDecision, res.Consensus.Outcome, res.Consensus.Round)
			wrote = true
		}
	}
	if !wrote {
		return extra
	}
	var out strings.Builder
	out.WriteString("[Conversation history]\n")
	out.WriteString(history.String())
	if strings.TrimSpace(extra) != "" {
		out.WriteString("\n[Additional background]\n")
		out.WriteString(extra)
	}
	return out.String()
}
