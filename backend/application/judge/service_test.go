package judge

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

type fakeJudgeModel struct {
	content string
}

func (m *fakeJudgeModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.content, nil), nil
}
func (m *fakeJudgeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}
func (m *fakeJudgeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type fakeJudgeModelPort struct {
	content string
}

func (p *fakeJudgeModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return &fakeJudgeModel{content: p.content}, nil
}

type fakeResRepo struct{ res *entity.Resolution }
type fakeVoteRepo struct{ vs []*entity.Vote }
type fakeEvRepo struct{ evs []*entity.EvidenceRecord }
type fakeClaimRepo struct{ cls []*entity.Claim }
type fakeRefRepo struct{ rfs []*entity.Reflection }
type fakeJudgeRepo struct{ saved *entity.JudgeResult }

func (r *fakeResRepo) Create(ctx context.Context, res *entity.Resolution) error { return nil }
func (r *fakeResRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	if r.res == nil {
		return nil, errors.New("not found")
	}
	return r.res, nil
}
func (r *fakeVoteRepo) Create(ctx context.Context, v *entity.Vote) error { return nil }
func (r *fakeVoteRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Vote, error) {
	return r.vs, nil
}
func (r *fakeEvRepo) Get(ctx context.Context, id string) (*entity.EvidenceRecord, error) {
	return nil, nil
}
func (r *fakeEvRepo) Create(ctx context.Context, e *entity.EvidenceRecord) error { return nil }
func (r *fakeEvRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error) {
	return r.evs, nil
}
func (r *fakeClaimRepo) Get(ctx context.Context, id string) (*entity.Claim, error) { return nil, nil }
func (r *fakeClaimRepo) Create(ctx context.Context, c *entity.Claim) error         { return nil }
func (r *fakeClaimRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Claim, error) {
	return r.cls, nil
}
func (r *fakeRefRepo) Create(ctx context.Context, rf *entity.Reflection) error { return nil }
func (r *fakeRefRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Reflection, error) {
	return r.rfs, nil
}
func (r *fakeJudgeRepo) Save(ctx context.Context, j *entity.JudgeResult) error {
	r.saved = j
	return nil
}
func (r *fakeJudgeRepo) GetLatest(ctx context.Context, caseID string) (*entity.JudgeResult, error) {
	if r.saved == nil {
		return nil, errors.New("not found")
	}
	return r.saved, nil
}

func TestService_JudgePersistsVerdict(t *testing.T) {
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	jv, err := validation.NewTypedValidator[JudgeOutput](gen, val)
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	judgeRepo := &fakeJudgeRepo{}
	svc := &Service{
		modelPort:   &fakeJudgeModelPort{content: `{"report_quality":90,"evidence_consistency":85,"reflection_validity":80,"overall":88,"rationale":"solid"}`},
		modelRef:    entity.ModelRef{ModelName: "judge-model"},
		gen:         gen,
		val:         val,
		judgeVal:    jv,
		repo:        judgeRepo,
		resolution:  &fakeResRepo{res: &entity.Resolution{CaseID: "c1", FinalDecision: entity.VoteDecisionApprove, FinalReport: "report"}},
		votes:       &fakeVoteRepo{vs: []*entity.Vote{{Decision: entity.VoteDecisionApprove}}},
		evidence:    &fakeEvRepo{},
		claims:      &fakeClaimRepo{},
		reflections: &fakeRefRepo{},
	}
	res, err := svc.Judge(context.Background(), "c1")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if res.Overall != 88 || res.ReportQuality != 90 || res.EvidenceConsistency != 85 || res.ReflectionValidity != 80 {
		t.Fatalf("verdict: %+v", res)
	}
	if res.ModelName != "judge-model" || judgeRepo.saved == nil || judgeRepo.saved.CaseID != "c1" {
		t.Fatalf("persist: %+v", judgeRepo.saved)
	}
}

func TestService_JudgeRejectsOutOfRangeVerdict(t *testing.T) {
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	jv, _ := validation.NewTypedValidator[JudgeOutput](gen, val)
	svc := &Service{
		modelPort:   &fakeJudgeModelPort{content: `{"overall":-5,"report_quality":10,"evidence_consistency":10,"reflection_validity":10}`},
		modelRef:    entity.ModelRef{},
		gen:         gen,
		val:         val,
		judgeVal:    jv,
		resolution:  &fakeResRepo{res: &entity.Resolution{CaseID: "c2"}},
		votes:       &fakeVoteRepo{},
		evidence:    &fakeEvRepo{},
		claims:      &fakeClaimRepo{},
		reflections: &fakeRefRepo{},
	}
	if _, err := svc.Judge(context.Background(), "c2"); err == nil {
		t.Fatal("expected out-of-range verdict to fail")
	}
}
