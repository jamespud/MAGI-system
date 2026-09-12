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

// authorize checks that the authenticated principal may access the case.
func (h *ArtifactHandler) authorize(ctx context.Context, c *app.RequestContext, id string) bool {
	case_, _ := h.svc.Get(ctx, id)
	if CaseAllowed(ctx, case_) {
		return true
	}
	Forbidden(c)
	return false
}

func (h *ArtifactHandler) Evidence(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if !h.authorize(ctx, c, id) {
		return
	}
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
	if !h.authorize(ctx, c, id) {
		return
	}
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
	if !h.authorize(ctx, c, id) {
		return
	}
	vs, err := h.svc.Votes(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	// Resolve agent_code per vote by joining on AgentRun (the Vote entity only
	// carries AgentRunID). Without this, every vote's agent_code is empty and
	// the Evidence Graph collapses all vote nodes into one.
	runs, err := h.svc.AgentRuns(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
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
	if !h.authorize(ctx, c, id) {
		return
	}
	// Required reads: a repository error must surface as a 5xx, never as an
	// empty 200 that the UI would render as "no evidence".
	runs, err := h.svc.AgentRuns(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	evs, err := h.svc.Evidence(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	cls, err := h.svc.Claims(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	vs, err := h.svc.Votes(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	tcs, err := h.svc.ToolCalls(ctx, id)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}

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
	// Select the single latest run per agent, then build every field of the
	// snapshot from that one run. Aggregating status/round from an
	// unspecified row order while joining tool calls to the latest run would
	// otherwise splice different rounds into one snapshot.
	latestRunByAgent := map[string]*entity.AgentRun{}
	for _, r := range runs {
		code := string(r.MagiCode)
		cur, ok := latestRunByAgent[code]
		if !ok || runIsNewer(r, cur) {
			latestRunByAgent[code] = r
		}
	}

	out := make(map[string]dto.AgentSnapshotDTO, len(latestRunByAgent))
	for code, r := range latestRunByAgent {
		toolCalls := tcByRun[r.ID]
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

// runIsNewer is the total order used to pick an agent's authoritative latest
// run: higher round wins, then later start, then larger id. The id tie-break is
// what makes the choice deterministic when a crash/retry leaves two runs with
// the same round and start time (agent runs carry no attempt counter).
func runIsNewer(a, b *entity.AgentRun) bool {
	if a.Round != b.Round {
		return a.Round > b.Round
	}
	if !a.StartedAt.Equal(b.StartedAt) {
		return a.StartedAt.After(b.StartedAt)
	}
	return a.ID > b.ID
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
