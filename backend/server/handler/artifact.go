package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

// ArtifactHandler serves the derived artifact endpoints: /agents, /evidence,
// /claims, /votes for a case.
type ArtifactHandler struct {
	svc *decision.Service
}

func NewArtifactHandler(svc *decision.Service) *ArtifactHandler {
	return &ArtifactHandler{svc: svc}
}

func (h *ArtifactHandler) Evidence(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	evs, err := h.svc.Evidence(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.EvidenceDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, dto.FromEvidence(e))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *ArtifactHandler) Claims(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	cls, err := h.svc.Claims(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.ClaimDTO, 0, len(cls))
	for _, cl := range cls {
		out = append(out, dto.FromClaim(cl))
	}
	c.JSON(consts.StatusOK, out)
}

func (h *ArtifactHandler) Votes(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	vs, err := h.svc.Votes(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	// Resolve agent_code per vote by joining on AgentRun (the Vote entity only
	// carries AgentRunID). Without this, every vote's agent_code is empty and
	// the Evidence Graph collapses all vote nodes into one.
	runs, _ := h.svc.AgentRuns(ctx, id)
	runCode := map[string]string{}
	for _, r := range runs {
		runCode[r.ID] = string(r.MagiCode)
	}
	out := make([]dto.VoteDTO, 0, len(vs))
	for _, v := range vs {
		out = append(out, dto.FromVote(v, runCode[v.AgentRunID]))
	}
	c.JSON(consts.StatusOK, out)
}

// Agents returns a per-agent snapshot aggregating run status + evidence/claim
// counts + the agent's latest vote.
func (h *ArtifactHandler) Agents(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	runs, _ := h.svc.AgentRuns(ctx, id)
	evs, _ := h.svc.Evidence(ctx, id)
	cls, _ := h.svc.Claims(ctx, id)
	vs, _ := h.svc.Votes(ctx, id)
	tcs, _ := h.svc.ToolCalls(ctx, id)

	evByAgent := map[string][]dto.EvidenceDTO{}
	for _, e := range evs {
		code := string(e.CollectedBy)
		evByAgent[code] = append(evByAgent[code], dto.FromEvidence(e))
	}
	clByAgent := map[string][]dto.ClaimDTO{}
	for _, cl := range cls {
		code := string(cl.CreatedBy)
		clByAgent[code] = append(clByAgent[code], dto.FromClaim(cl))
	}
	tcByRun := map[string][]dto.ToolCallDTO{}
	for _, tc := range tcs {
		tcByRun[tc.AgentRunID] = append(tcByRun[tc.AgentRunID], dto.FromToolCall(tc))
	}
	// latest vote per agent (by round)
	voteByAgent := map[string]*dto.VoteDTO{}
	for _, v := range vs {
		key := agentCodeFromRun(runs, v)
		if key == "" {
			continue
		}
		vd := dto.FromVote(v, key)
		if cur, ok := voteByAgent[key]; !ok || v.Round >= cur.Round {
			voteByAgent[key] = &vd
		}
	}
	// latest run id per agent code (so tool calls join to the right run)
	runIDByAgent := map[string]string{}
	runRoundByAgent := map[string]int{}
	for _, r := range runs {
		code := string(r.MagiCode)
		if _, ok := runIDByAgent[code]; !ok || r.Round >= runRoundByAgent[code] {
			runIDByAgent[code] = r.ID
			runRoundByAgent[code] = r.Round
		}
	}

	out := make(map[string]dto.AgentSnapshotDTO, len(runs))
	for _, r := range runs {
		code := string(r.MagiCode)
		runID := runIDByAgent[code]
		toolCalls := tcByRun[runID]
		if toolCalls == nil {
			toolCalls = []dto.ToolCallDTO{}
		}
		evidence := evByAgent[code]
		if evidence == nil {
			evidence = []dto.EvidenceDTO{}
		}
		claims := clByAgent[code]
		if claims == nil {
			claims = []dto.ClaimDTO{}
		}
		snap := dto.AgentSnapshotDTO{
			AgentCode: code,
			Status:    string(r.Status),
			Round:     r.Round,
			Step:      len(toolCalls),
			ToolCalls: toolCalls,
			Evidence:  evidence,
			Claims:    claims,
		}
		if v, ok := voteByAgent[code]; ok {
			snap.Vote = v
		}
		out[code] = snap
	}
	c.JSON(consts.StatusOK, out)
}

// agentCodeFromRun resolves the agent code for a vote by joining on AgentRunID
// when available; votes persisted with a known agent code carry it directly.
func agentCodeFromRun(runs []*entity.AgentRun, v *entity.Vote) string {
	for _, r := range runs {
		if r.ID == v.AgentRunID {
			return string(r.MagiCode)
		}
	}
	return ""
}
