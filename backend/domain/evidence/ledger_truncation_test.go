package evidence

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

// evidence_record.source_uri is VARCHAR(512): an oversized URL used to fail the
// INSERT (1406) and lose the whole evidence row.
func TestLedgerRecord_TruncatesOversizedSourceURI(t *testing.T) {
	ledger := NewEvidenceLedger("case-1", "run-1", "melchior")
	long := "https://example.com/" + strings.Repeat("u", 600)
	ev := ledger.Record("tc-1", "web_fetch", "local", long, "obs", entity.ReliabilityScore{Final: 0.9})
	if ev.SourceURI == nil {
		t.Fatal("source uri dropped")
	}
	if len(*ev.SourceURI) > 512 {
		t.Fatalf("source uri length = %d, want <= 512", len(*ev.SourceURI))
	}
	if !strings.HasPrefix(*ev.SourceURI, "https://example.com/") {
		t.Fatalf("truncation mangled the url: %q", *ev.SourceURI)
	}
	// The observation, which agents actually cite, must survive intact.
	if ev.Observation != "obs" {
		t.Fatalf("observation = %q", ev.Observation)
	}
}
