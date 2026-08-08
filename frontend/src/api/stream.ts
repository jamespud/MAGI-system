import { useCaseStore, useEventStore, useAgentStore } from '@/stores';
import { mapBackendEvent } from './eventMapper';
import type { ApiEvent } from './client';
import type { AgentId, AgentStatus } from '@/types/agent';
import type { CaseStatus } from '@/types/case';

// Maps backend event types to the agent status they imply. Absent types
// (e.g. TASK_NORMALIZED) leave the agent status untouched.
const STATUS_BY_TYPE: Record<string, AgentStatus> = {
  AGENT_STARTED: 'running',
  MODEL_REQUESTED: 'running',
  MODEL_RESPONDED: 'running',
  TOOL_CALL_REQUESTED: 'running',
  TOOL_CALL_STARTED: 'running',
  EVIDENCE_CREATED: 'running',
  VOTE_SUBMITTED: 'completed',
  CASE_COMPLETED: 'completed',
  CASE_FAILED: 'error',
  EVIDENCE_GATE_FAILED: 'error',
};

// Terminal backend event types: when these arrive the run is over and the
// caller should re-fetch the case + artifacts (status/consensus/votes are
// only final after completion).
const TERMINAL_TYPES = new Set(['CASE_COMPLETED', 'CASE_FAILED']);

// subscribeCaseStream opens an SSE connection to /cases/:id/stream, maps each
// backend event to the frontend shape, pushes it into eventStore, and patches
// agentStore status for agent-scoped events. onTerminal (optional) is called
// once when a terminal event (CASE_COMPLETED/CASE_FAILED) arrives so the caller
// can refresh case detail + artifacts. Returns an unsubscribe that closes the
// connection.
export function subscribeCaseStream(caseId: string, onTerminal?: () => void): () => void {
  const es = new EventSource(`/api/v1/cases/${caseId}/stream`);
  es.onmessage = (msg: MessageEvent) => {
    let raw: ApiEvent;
    try {
      raw = JSON.parse(msg.data) as ApiEvent;
    } catch {
      return; // ignore malformed frames
    }
    if (raw.type === 'CASE_STATUS_CHANGED') {
      const status = raw.payload?.status;
      const round = raw.payload?.round;
      if (typeof status === 'string') {
        useCaseStore.getState().updateCaseStatus(caseId, status as CaseStatus, typeof round === 'number' ? round : 0);
      }
      return; // phase transitions update the case status, not the timeline
    }
    const ev = mapBackendEvent(raw);
    useEventStore.getState().pushEvent(ev);
    if (ev.agentId) {
      const status = STATUS_BY_TYPE[raw.type];
      if (status) {
        // patchAgent (not updateAgentStatus) so a snapshot is created when the
        // agent is first seen mid-stream rather than left null.
        useAgentStore.getState().patchAgent(ev.agentId as AgentId, { status });
      }
    }
    if (onTerminal && TERMINAL_TYPES.has(raw.type)) {
      onTerminal();
    }
  };
  es.onerror = () => {
    // EventSource auto-reconnects; nothing to surface here.
  };
  return () => es.close();
}
