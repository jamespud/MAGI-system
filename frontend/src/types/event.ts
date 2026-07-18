import type { AgentId } from './agent';

export type EventType =
  | 'TOOL_CALL'
  | 'AGENT_STEP'
  | 'EVIDENCE_CREATED'
  | 'VOTE_SUBMITTED'
  | 'CONSENSUS_CHANGED'
  | 'ROUND_START'
  | 'DEBATE_START'
  | 'REFLECTION'
  | 'RESOLVED'
  | 'ERROR';

export const EVENT_TYPE_LABELS: Record<EventType, string> = {
  TOOL_CALL: 'Tool Call',
  AGENT_STEP: 'Agent Step',
  EVIDENCE_CREATED: 'Evidence Created',
  VOTE_SUBMITTED: 'Vote Submitted',
  CONSENSUS_CHANGED: 'Consensus Changed',
  ROUND_START: 'Round Start',
  DEBATE_START: 'Debate Start',
  REFLECTION: 'Reflection',
  RESOLVED: 'Resolved',
  ERROR: 'Error',
};

export interface MagiEvent {
  id: string;
  type: EventType;
  timestamp: string;
  agentId?: AgentId;
  message: string;
  data?: Record<string, unknown>;
}

export interface EventFilter {
  tool: boolean;
  agent: boolean;
  evidence: boolean;
  vote: boolean;
}
