package entity

import "time"

// KnowledgeDoc is a user-uploaded knowledge document indexed into the RAG
// pipeline for retrieval by agents and the memory search.
type KnowledgeDoc struct {
	ID         string
	UserID     int64
	Title      string
	Content    string
	SourceKind string // "text" | "url"
	SourceURL  string
	Status     string // "indexed" | "failed"
	Error      string
	Chunks     int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const (
	KnowledgeSourceText = "text"
	KnowledgeSourceURL  = "url"

	KnowledgeStatusIndexed = "indexed"
	KnowledgeStatusFailed  = "failed"
)
