import type { MagiEvent, EventType } from '@/types/event';
import type { ApiEvent } from './client';
import type { AgentId } from '@/types/agent';

// Maps the backend's 25 event types to the frontend's 10 display types.
// Backend type names are longer (TOOL_CALL_REQUESTED vs TOOL_CALL); the
// frontend collapses related backend events into one display bucket.
const TYPE_MAP: Record<string, EventType> = {
  CASE_CREATED: 'ROUND_START',
  TASK_NORMALIZED: 'ROUND_START',
  MEMORY_RETRIEVED: 'ROUND_START',
  AGENT_STARTED: 'AGENT_STEP',
  MODEL_REQUESTED: 'AGENT_STEP',
  MODEL_RESPONDED: 'AGENT_STEP',
  TOOL_CALL_REQUESTED: 'TOOL_CALL',
  TOOL_CALL_VALIDATED: 'TOOL_CALL',
  TOOL_CALL_STARTED: 'TOOL_CALL',
  TOOL_CALL_COMPLETED: 'TOOL_CALL',
  TOOL_CALL_FAILED: 'TOOL_CALL',
  EVIDENCE_CREATED: 'EVIDENCE_CREATED',
  CLAIM_CREATED: 'EVIDENCE_CREATED',
  EVIDENCE_GATE_PASSED: 'AGENT_STEP',
  VOTE_SUBMITTED: 'VOTE_SUBMITTED',
  REVOTE_SUBMITTED: 'VOTE_SUBMITTED',
  CONSENSUS_EVALUATED: 'CONSENSUS_CHANGED',
  DEBATE_STARTED: 'DEBATE_START',
  REFLECTION_SUBMITTED: 'REFLECTION',
  RESOLUTION_CREATED: 'RESOLVED',
  MEMORY_INDEXED: 'RESOLVED',
  CASE_COMPLETED: 'RESOLVED',
  CASE_FAILED: 'ERROR',
  EVIDENCE_GATE_FAILED: 'ERROR',
  CLAIM_CONTRADICTION_DECLARED: 'ERROR',
};

export function mapBackendEvent(raw: ApiEvent): MagiEvent {
  const type = TYPE_MAP[raw.type] ?? 'ERROR';
  const agentId = (raw.agent_code as AgentId | undefined) ?? undefined;
  return {
    id: raw.id,
    type,
    timestamp: raw.timestamp,
    agentId,
    message: raw.message,
    data: raw.payload,
  };
}
