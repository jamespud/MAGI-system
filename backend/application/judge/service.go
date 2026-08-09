package judge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

// JudgeOutput is the structured LLM-as-a-Judge verdict. Scores are 0-100.
type JudgeOutput struct {
	ReportQuality       float64 `json:"report_quality"`
	EvidenceConsistency float64 `json:"evidence_consistency"`
	ReflectionValidity  float64 `json:"reflection_validity"`
	Overall             float64 `json:"overall"`
	Rationale           string  `json:"rationale"`
}

// Service runs semantic evaluation of a completed case via an LLM judge.
type Service struct {
	modelPort port.ModelPort
	modelRef  entity.ModelRef
	gen       validation.SchemaGenerator
	val       validation.Validator
	judgeVal  *validation.TypedValidator[JudgeOutput]
	repo      port.JudgeRepository
	agentRuns port.AgentRunRepository
	evidence  port.EvidenceRepository
	votes     port.VoteRepository
	claims    port.ClaimRepository
	reflections port.ReflectionRepository
	resolution port.ResolutionRepository
}

func NewService(modelPort port.ModelPort, modelRef entity.ModelRef, gen validation.SchemaGenerator, val validation.Validator, repo port.JudgeRepository) (*Service, error) {
	jv, err := validation.NewTypedValidator[JudgeOutput](gen, val)
	if err != nil {
		return nil, fmt.Errorf("judge: validator: %w", err)
	}
	return &Service{modelPort: modelPort, modelRef: modelRef, gen: gen, val: val, judgeVal: jv, repo: repo}, nil
}

// WithRepositories wires persisted case artifacts used to build the judge prompt.
func (s *Service) WithRepositories(repo port.Repository) *Service {
	s.agentRuns = repo.AgentRunRepo()
	s.evidence = repo.EvidenceRepo()
	s.votes = repo.VoteRepo()
	s.claims = repo.ClaimRepo()
	s.reflections = repo.ReflectionRepo()
	s.resolution = repo.ResolutionRepo()
	return s
}

// Judge evaluates a completed case and persists the verdict.
func (s *Service) Judge(ctx context.Context, caseID string) (*entity.JudgeResult, error) {
	if caseID == "" {
		return nil, fmt.Errorf("judge: case ID is required")
	}
	if s.modelPort == nil || s.judgeVal == nil {
		return nil, fmt.Errorf("judge: service is not configured")
	}
	resolution, err := s.resolution.Get(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("judge: resolution: %w", err)
	}
	votes, err := s.votes.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("judge: votes: %w", err)
	}
	evidence, err := s.evidence.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("judge: evidence: %w", err)
	}
	claims, err := s.claims.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("judge: claims: %w", err)
	}
	reflections, err := s.reflections.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("judge: reflections: %w", err)
	}

	prompt := buildJudgePrompt(caseID, resolution, votes, evidence, claims, reflections)
	cm, err := s.modelPort.Build(ctx, s.modelRef)
	if err != nil {
		return nil, fmt.Errorf("judge: build model: %w", err)
	}
	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You are a strict decision-quality judge. Score each dimension 0-100 and return JSON only."),
		schema.UserMessage(prompt),
	})
	if err != nil {
		return nil, fmt.Errorf("judge: generate: %w", err)
	}
	out, vr := s.judgeVal.ValidateAndUnmarshal([]byte(resp.Content))
	if vr == nil || !vr.Valid {
		return nil, fmt.Errorf("judge: invalid verdict: %v", vr)
	}
	if err := validateScores(*out); err != nil {
		return nil, err
	}
	res := &entity.JudgeResult{
		CaseID: caseID, ReportQuality: out.ReportQuality, EvidenceConsistency: out.EvidenceConsistency,
		ReflectionValidity: out.ReflectionValidity, Overall: out.Overall, Rationale: out.Rationale,
		ModelName: s.modelRef.ModelName, CreatedAt: time.Now(),
	}
	if s.repo != nil {
		if err := s.repo.Save(ctx, res); err != nil {
			return nil, fmt.Errorf("judge: save: %w", err)
		}
	}
	return res, nil
}

func buildJudgePrompt(caseID string, resolution *entity.Resolution, votes []*entity.Vote, evidence []*entity.EvidenceRecord, claims []*entity.Claim, reflections []*entity.Reflection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Case: %s\n", caseID)
	if resolution != nil {
		fmt.Fprintf(&b, "Final decision: %s\nReport:\n%s\n", resolution.FinalDecision, truncate(resolution.FinalReport, 4000))
	}
	if len(votes) > 0 {
		b.WriteString("\nVotes:\n")
		for _, v := range votes {
			fmt.Fprintf(&b, "- %s confidence=%.0f reasoning=%q evidence=%v\n", v.Decision, v.Confidence, v.ReasoningSummary, v.EvidenceIDs)
		}
	}
	if len(evidence) > 0 {
		b.WriteString("\nEvidence:\n")
		for i, e := range evidence {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- %s type=%s rel=%.2f obs=%q\n", e.ID, e.SourceType, e.Reliability.Final, truncate(e.Observation, 300))
		}
	}
	if len(claims) > 0 {
		b.WriteString("\nClaims:\n")
		for i, c := range claims {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- %s by=%s supports=%v contradicts=%v\n", c.Statement, c.CreatedBy, c.Supports, c.Contradicts)
		}
	}
	if len(reflections) > 0 {
		b.WriteString("\nReflections:\n")
		for i, r := range reflections {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- change=%s accepted=%v rejected=%v new_evidence=%v reasoning=%q\n", r.PositionChange, r.AcceptedClaims, r.RejectedClaims, r.NewEvidenceIDs, truncate(r.Reasoning, 300))
		}
	}
	b.WriteString("\nScore: report_quality, evidence_consistency (do cited evidence support votes/report?), reflection_validity (are position changes justified?), overall; add a short rationale.")
	return b.String()
}
 
func validateScores(out JudgeOutput) error {
	for name, v := range map[string]float64{
		"report_quality": out.ReportQuality, "evidence_consistency": out.EvidenceConsistency,
		"reflection_validity": out.ReflectionValidity, "overall": out.Overall,
	} {
		if v < 0 || v > 100 {
			return fmt.Errorf("judge: %s out of range [0,100]: %v", name, v)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
