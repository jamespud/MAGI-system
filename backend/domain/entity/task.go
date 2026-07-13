package entity

// DecisionTask is the Commander-normalized task package derived from a DecisionCase.
type DecisionTask struct {
	CanonicalQuestion string              `json:"canonical_question"`
	DecisionType      DecisionType        `json:"decision_type,omitempty"`
	Background        string              `json:"background,omitempty"`
	Constraints       []Constraint        `json:"constraints,omitempty"`
	Dimensions        []DecisionDimension `json:"dimensions,omitempty"`
	InformationNeeds  []InformationNeed   `json:"information_needs,omitempty"`
	SuccessCriteria   []Criterion         `json:"success_criteria,omitempty"`
	Unknowns          []string            `json:"unknowns,omitempty"`
}

type DecisionType string

const (
	DecisionTypeAdopt      DecisionType = "adopt"
	DecisionTypeMigrate    DecisionType = "migrate"
	DecisionTypeLaunch     DecisionType = "launch"
	DecisionTypeStrategic  DecisionType = "strategic"
	DecisionTypeGeneric    DecisionType = "generic"
)

type DecisionDimension struct {
	Code        string
	Description string
}

type InformationNeed struct {
	Topic     string
	Rationale string
}

type Criterion struct {
	Code        string
	Description string
}
