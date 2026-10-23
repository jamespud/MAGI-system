package inputartifact

func validateBudget(b Budget) error {
	for _, n := range []int64{b.RoleToolCalls, b.TaskToolCalls, b.RoleOutputTokens, b.TaskOutputTokens, b.TaskInputTokens} {
		if n <= 0 || !safeUint(n) {
			return invalid("BUDGET", "$.budget", "all resource caps must be positive safe integers")
		}
	}
	if b.CostLimit != nil {
		return validateMoney(*b.CostLimit, "$.budget.cost_limit")
	}
	return nil
}

// ComposeBudgets intersects supplied caps. Consumers must supply all applicable
// Charter, Persona and engineering limits before execution. It grants no rights.
func ComposeBudgets(first Budget, others ...Budget) (Budget, error) {
	if e := validateBudget(first); e != nil {
		return Budget{}, e
	}
	result := first
	if result.CostLimit != nil {
		m := *result.CostLimit
		result.CostLimit = &m
	}
	for _, b := range others {
		if e := validateBudget(b); e != nil {
			return Budget{}, e
		}
		result.RoleToolCalls = min(result.RoleToolCalls, b.RoleToolCalls)
		result.TaskToolCalls = min(result.TaskToolCalls, b.TaskToolCalls)
		result.RoleOutputTokens = min(result.RoleOutputTokens, b.RoleOutputTokens)
		result.TaskOutputTokens = min(result.TaskOutputTokens, b.TaskOutputTokens)
		result.TaskInputTokens = min(result.TaskInputTokens, b.TaskInputTokens)
		if b.CostLimit != nil {
			if result.CostLimit != nil && (result.CostLimit.Currency != b.CostLimit.Currency || result.CostLimit.MinorUnitExponent != b.CostLimit.MinorUnitExponent) {
				return Budget{}, invalid("MONEY", "$.budget.cost_limit", "billing dimensions differ; cannot infer currency conversion")
			}
			if result.CostLimit == nil || b.CostLimit.MinorUnits < result.CostLimit.MinorUnits {
				m := *b.CostLimit
				result.CostLimit = &m
			}
		}
	}
	result.RoleToolCalls = min(result.RoleToolCalls, result.TaskToolCalls)
	result.RoleOutputTokens = min(result.RoleOutputTokens, result.TaskOutputTokens)
	return result, nil
}

// ComposePersonaV1Budget applies the fixed resource ceiling only; it does not
// validate PersonaSpec or assert baseline acceptance or execution permission.
func ComposePersonaV1Budget(domain Budget) (Budget, error) {
	b, e := ComposeBudgets(domain)
	if e != nil {
		return Budget{}, e
	}
	b.RoleToolCalls = min(b.RoleToolCalls, 6)
	b.RoleOutputTokens = min(b.RoleOutputTokens, 6000)
	return b, nil
}
