package validation

// Column-size contracts shared by the application layer and the schema.
//
// These mirror the VARCHAR widths declared by the Atlas migrations and the GORM
// models; the migration remains the source of truth. They live here so that a
// service can reject an oversized value with a business error instead of
// letting MySQL raise 1406 half way through a write, and so that changing a
// column width is a single edit rather than a hunt for every copy of the
// number. Values that are counted in runes are named ...Rune; values that are
// counted in bytes (URLs and generated identities, which the existing call sites
// already measured in bytes) are named ...Bytes.
const (
	// MaxUserNameRune mirrors users.name.
	MaxUserNameRune = 191
	// MaxUserEmailRune mirrors users.email, which is deliberately wider than the
	// indexable 191 because it is not uniquely indexed yet.
	MaxUserEmailRune = 255

	// MaxKnowledgeTitleRune mirrors knowledge_docs.title.
	MaxKnowledgeTitleRune = 191
	// MaxKnowledgeSourceURLRune mirrors knowledge_docs.source_url.
	MaxKnowledgeSourceURLRune = 191

	// MaxRecurringCaseNameRune mirrors recurring_case.name.
	MaxRecurringCaseNameRune = 191

	// MaxEvidenceSourceURIBytes mirrors evidence_record.source_uri.
	MaxEvidenceSourceURIBytes = 512

	// MaxInvocationRunIDBytes mirrors runtime_invocation.run_id and
	// runtime_invocation_attempt.attempt_id.
	MaxInvocationRunIDBytes = 191
)
