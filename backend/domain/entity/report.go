package entity

// FinalReportData is the Commander's structured, schema-validated output for
// the final decision report (design §9). It carries only natural-language
// explanation plus citations: the authoritative Decision and the minority
// positions are bound deterministically from the resolution, never authored by
// the report model (invariant REPORT-01, issue #13).
type FinalReportData struct {
	Summary        string   `json:"summary"`
	KeyReasons     []string `json:"key_reasons"`
	Risks          []string `json:"risks"`
	NextSteps      []string `json:"next_steps"`
	KeyEvidenceIDs []string `json:"key_evidence_ids,omitempty"`
	KeyClaimIDs    []string `json:"key_claim_ids,omitempty"`
}
