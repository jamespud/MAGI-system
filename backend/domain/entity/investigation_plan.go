package entity

import "time"

// InvestigationPlanItem is one sub-investigation in a case plan.
type InvestigationPlanItem struct {
	Question   string `json:"question"`
	Background string `json:"background,omitempty"`
}

// InvestigationPlan is the editable investigation plan for a case: the list
// of sub-questions the agents should investigate. It feeds the parallel
// `delegate` tool and the task state tree.
type InvestigationPlan struct {
	CaseID    string
	Items     []InvestigationPlanItem
	UpdatedAt time.Time
}
