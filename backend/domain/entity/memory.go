package entity

// CaseMemoryProjection is the long-term memory projection of a closed case,
// stored via crossknowledge for future RAG retrieval (ADR-006).
type CaseMemoryProjection struct {
	CaseID            string
	QuestionSummary   string
	ContextSummary    string
	KeyEvidence       []MemoryEvidence
	KeyClaims         []MemoryClaim
	Votes             []MemoryVote
	Resolution        string
	Outcome           *CaseOutcome
	Annotation        string
	Tags              []string
	ProjectionVersion int
	// IndexDoc is the full-text document used for RAG indexing only. It is
	// carried in memory (AsyncIndexer job / sync Store) and is NOT persisted
	// to the case_memory_projection table, which keeps truncated display fields.
	IndexDoc string
}

type CaseOutcome struct {
	Status  string
	Learned string
}

type MemoryEvidence struct {
	EvidenceID  string
	Observation string
	Reliability float64
}

type MemoryClaim struct {
	ClaimID   string
	Statement string
}

type MemoryVote struct {
	MagiCode   MagiCode
	Decision   VoteDecision
	Confidence float64
}
