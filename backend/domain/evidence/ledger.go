package evidence

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

// EvidenceLedger is the in-memory per-run store of EvidenceRecords and Claims.
type EvidenceLedger struct {
	mu          sync.Mutex
	records     map[string]*entity.EvidenceRecord
	recordOrder []string
	claims      map[string]*entity.Claim
	claimOrder  []string
	evCounter   int
	clCounter   int
	caseID      string
	agentRunID  string
	collector   string
}

type ledgerSnapshot struct {
	CaseID          string                   `json:"case_id"`
	AgentRunID      string                   `json:"agent_run_id"`
	Collector       string                   `json:"collector"`
	Records         []*entity.EvidenceRecord `json:"records"`
	Claims          []*entity.Claim          `json:"claims"`
	EvidenceCounter int                      `json:"evidence_counter"`
	ClaimCounter    int                      `json:"claim_counter"`
}

func NewEvidenceLedger(caseID, agentRunID, collector string) *EvidenceLedger {
	return &EvidenceLedger{
		records: make(map[string]*entity.EvidenceRecord),
		claims:  make(map[string]*entity.Claim),
		caseID:  caseID, agentRunID: agentRunID, collector: collector,
	}
}

// MarshalLedger serializes the ledger without changing its business meaning.
// The counters and insertion order are included so restored identifiers remain
// stable when the loop records more evidence or claims.
func MarshalLedger(ledger *EvidenceLedger) (string, error) {
	if ledger == nil {
		return "", fmt.Errorf("marshal ledger: nil ledger")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	records := make([]*entity.EvidenceRecord, 0, len(ledger.recordOrder))
	for _, id := range ledger.recordOrder {
		records = append(records, ledger.records[id])
	}
	claims := make([]*entity.Claim, 0, len(ledger.claimOrder))
	for _, id := range ledger.claimOrder {
		claims = append(claims, ledger.claims[id])
	}
	encoded, err := json.Marshal(ledgerSnapshot{
		CaseID: ledger.caseID, AgentRunID: ledger.agentRunID, Collector: ledger.collector,
		Records: records, Claims: claims, EvidenceCounter: ledger.evCounter, ClaimCounter: ledger.clCounter,
	})
	if err != nil {
		return "", fmt.Errorf("marshal ledger: %w", err)
	}
	return string(encoded), nil
}

// RestoreLedger reverses MarshalLedger. It restores ordering and counters so
// record/claim IDs are not reused after a checkpoint resume.
func RestoreLedger(encoded string) (*EvidenceLedger, error) {
	var snapshot ledgerSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return nil, fmt.Errorf("restore ledger: %w", err)
	}
	if snapshot.Records == nil || snapshot.Claims == nil {
		return nil, fmt.Errorf("restore ledger: incomplete state")
	}
	ledger := NewEvidenceLedger(snapshot.CaseID, snapshot.AgentRunID, snapshot.Collector)
	ledger.evCounter = snapshot.EvidenceCounter
	ledger.clCounter = snapshot.ClaimCounter
	for _, record := range snapshot.Records {
		if record == nil || record.ID == "" {
			return nil, fmt.Errorf("restore ledger: invalid evidence record")
		}
		if _, exists := ledger.records[record.ID]; exists {
			return nil, fmt.Errorf("restore ledger: duplicate evidence record %q", record.ID)
		}
		ledger.records[record.ID] = record
		ledger.recordOrder = append(ledger.recordOrder, record.ID)
	}
	for _, claim := range snapshot.Claims {
		if claim == nil || claim.ID == "" {
			return nil, fmt.Errorf("restore ledger: invalid claim")
		}
		if _, exists := ledger.claims[claim.ID]; exists {
			return nil, fmt.Errorf("restore ledger: duplicate claim %q", claim.ID)
		}
		ledger.claims[claim.ID] = claim
		ledger.claimOrder = append(ledger.claimOrder, claim.ID)
	}
	return ledger, nil
}

func (l *EvidenceLedger) Record(toolCallID, toolName, sourceType, sourceURI, observation string, reliability entity.ReliabilityScore) *entity.EvidenceRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evCounter++
	// Truncating (rather than failing) keeps the observation, which is the part
	// agents actually cite.
	if len(sourceURI) > validation.MaxEvidenceSourceURIBytes {
		cut := validation.MaxEvidenceSourceURIBytes
		for cut > 0 && !utf8.ValidString(sourceURI[:cut]) {
			cut--
		}
		sourceURI = sourceURI[:cut]
	}
	ev := &entity.EvidenceRecord{
		ID: fmt.Sprintf("EV-%03d", l.evCounter), CaseID: l.caseID, AgentRunID: l.agentRunID,
		ToolCallID: toolCallID, ToolName: toolName, SourceType: entity.EvidenceSourceType(sourceType),
		SourceURI: &sourceURI, RawContent: observation, Observation: observation,
		Reliability: reliability, CollectedBy: entity.MagiCode(l.collector), CreatedAt: time.Now(),
	}
	l.records[ev.ID] = ev
	l.recordOrder = append(l.recordOrder, ev.ID)
	return ev
}

func (l *EvidenceLedger) RecordClaim(stmt string, supports, contradicts []string) *entity.Claim {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clCounter++
	cl := &entity.Claim{
		ID: fmt.Sprintf("CL-%03d", l.clCounter), CaseID: l.caseID, AgentRunID: l.agentRunID,
		Statement: stmt, Supports: supports, Contradicts: contradicts,
		Status: entity.ClaimStatusOpen, CreatedBy: entity.MagiCode(l.collector), CreatedAt: time.Now(),
	}
	l.claims[cl.ID] = cl
	l.claimOrder = append(l.claimOrder, cl.ID)
	return cl
}

func (l *EvidenceLedger) Get(id string) (*entity.EvidenceRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[id]
	return r, ok
}

func (l *EvidenceLedger) ExistsCollected(id, collector string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[id]
	if !ok {
		return false
	}
	if collector == "" {
		return true
	}
	return string(r.CollectedBy) == collector
}

func (l *EvidenceLedger) Reliability(id string) (float64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[id]
	if !ok {
		return 0, false
	}
	return r.Reliability.Final, true
}

func (l *EvidenceLedger) List() []*entity.EvidenceRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*entity.EvidenceRecord, 0, len(l.recordOrder))
	for _, id := range l.recordOrder {
		out = append(out, l.records[id])
	}
	return out
}

// RecomputeCorroboration updates each evidence record's Corroboration modifier
// based on how many claims (already recorded + summary claims not yet recorded)
// support it, then recomputes Final. Called before the evidence gate so
// MinReliability checks use real corroboration (§12).
func (l *EvidenceLedger) RecomputeCorroboration(summaryClaims []entity.EvidenceSummaryClaim) {
	l.mu.Lock()
	defer l.mu.Unlock()
	counts := make(map[string]int)
	for _, c := range l.claims {
		for _, evID := range c.Supports {
			counts[evID]++
		}
	}
	for _, c := range summaryClaims {
		for _, evID := range c.Supports {
			counts[evID]++
		}
	}
	for _, ev := range l.records {
		ev.Reliability.Corroboration = 0.5 + float64(counts[ev.ID])*0.1
		if ev.Reliability.Corroboration > 1.0 {
			ev.Reliability.Corroboration = 1.0
		}
		RecomputeFinal(&ev.Reliability)
	}
}

func (l *EvidenceLedger) ListClaims() []*entity.Claim {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*entity.Claim, 0, len(l.claimOrder))
	for _, id := range l.claimOrder {
		out = append(out, l.claims[id])
	}
	return out
}
