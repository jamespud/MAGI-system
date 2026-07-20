import { useEventStore, useAgentStore } from '@/stores';
import { mapBackendEvent } from './eventMapper';
import type { ApiEvent } from './client';
import type { AgentId, AgentStatus } from '@/types/agent';

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

// subscribeCaseStream opens an SSE connection to /cases/:id/stream, maps each
// backend event to the frontend shape, pushes it into eventStore, and patches
// agentStore status for agent-scoped events. Returns an unsubscribe that
// closes the connection.
export function subscribeCaseStream(caseId: string): () => void {
  const es = new EventSource(`/api/v1/cases/${caseId}/stream`);
  es.onmessage = (msg: MessageEvent) => {
    let raw: ApiEvent;
    try {
      raw = JSON.parse(msg.data) as ApiEvent;
    } catch {
      return; // ignore malformed frames
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
  };
  es.onerror = () => {
    // EventSource auto-reconnects; nothing to surface here.
  };
  return () => es.close();
}
