package entity

// FinalReportData is the Commander's structured, schema-validated output for
// the final decision report (design §9). RenderReport turns it into markdown.
type FinalReportData struct {
	Decision   string   `json:"decision"`
	Summary    string   `json:"summary"`
	KeyReasons []string `json:"key_reasons"`
	Risks      []string `json:"risks"`
	NextSteps  []string `json:"next_steps"`
}
