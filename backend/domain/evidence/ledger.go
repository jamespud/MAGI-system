package evidence

import (
	"fmt"
	"sync"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
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

func NewEvidenceLedger(caseID, agentRunID, collector string) *EvidenceLedger {
	return &EvidenceLedger{
		records: make(map[string]*entity.EvidenceRecord),
		claims:  make(map[string]*entity.Claim),
		caseID:  caseID, agentRunID: agentRunID, collector: collector,
	}
}

func (l *EvidenceLedger) Record(toolCallID, toolName, sourceType, sourceURI, observation string, reliability entity.ReliabilityScore) *entity.EvidenceRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evCounter++
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

func (l *EvidenceLedger) ListClaims() []*entity.Claim {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*entity.Claim, 0, len(l.claimOrder))
	for _, id := range l.claimOrder {
		out = append(out, l.claims[id])
	}
	return out
}
