package rolepolicy

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// RoleCodes are the editable role-contract keys.
var RoleCodes = []string{"melchior", "balthasar", "casper"}

// Service manages editable role-contract specifications. Stored specs
// override the built-in defaults used by the evidence gate.
type Service struct {
	repo port.RolePolicyRepository
}

func NewService(repo port.RolePolicyRepository) *Service {
	return &Service{repo: repo}
}

// Get returns the stored spec or the built-in default for a role.
func (s *Service) Get(ctx context.Context, code string) (*entity.RolePolicy, error) {
	if !validCode(code) {
		return nil, fmt.Errorf("rolepolicy: unknown role %q", code)
	}
	stored, err := s.repo.Get(ctx, code)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		return stored, nil
	}
	def := entity.DefaultRolePolicy(code)
	return &def, nil
}

// List returns all role contracts (stored or default).
func (s *Service) List(ctx context.Context) ([]*entity.RolePolicy, error) {
	out := make([]*entity.RolePolicy, 0, len(RoleCodes))
	for _, code := range RoleCodes {
		p, err := s.Get(ctx, code)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Save validates and persists a role contract, then returns it.
func (s *Service) Save(ctx context.Context, code string, p entity.RolePolicy) (*entity.RolePolicy, error) {
	if !validCode(code) {
		return nil, fmt.Errorf("rolepolicy: unknown role %q", code)
	}
	if p.Role == "" {
		p.Role = code
	}
	if p.Role != code {
		return nil, fmt.Errorf("rolepolicy: role %q does not match spec key %q", p.Role, code)
	}
	if p.EnforceAssessment {
		switch p.RequiredAssessment {
		case entity.RoleAssessmentTechnical, entity.RoleAssessmentRisk, entity.RoleAssessmentOpportunity:
		default:
			return nil, fmt.Errorf("rolepolicy: required_assessment must be technical/risk/opportunity when enforced")
		}
	}
	if err := s.repo.Save(ctx, code, p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Reset restores the built-in default for a role.
func (s *Service) Reset(ctx context.Context, code string) (*entity.RolePolicy, error) {
	if !validCode(code) {
		return nil, fmt.Errorf("rolepolicy: unknown role %q", code)
	}
	def := entity.DefaultRolePolicy(code)
	if err := s.repo.Save(ctx, code, def); err != nil {
		return nil, err
	}
	return &def, nil
}

func validCode(code string) bool {
	for _, c := range RoleCodes {
		if c == code {
			return true
		}
	}
	return false
}
