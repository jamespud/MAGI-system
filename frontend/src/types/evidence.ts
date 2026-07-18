import type { AgentId } from './agent';

export interface EvidenceRecord {
  id: string;
  source: string;
  url: string;
  observation: string;
  reliability: number;
  collectedBy: AgentId;
  timestamp: string;
}

export interface EvidenceNode {
  id: string;
  label: string;
  type: 'evidence' | 'claim' | 'vote';
  evidence?: EvidenceRecord;
  claim?: ClaimNode;
  vote?: VoteNode;
}

export interface ClaimNode {
  id: string;
  text: string;
  agentId: AgentId;
}

export interface VoteNode {
  id: string;
  stance: string;
  agentId: AgentId;
}

export interface EvidenceEdge {
  source: string;
  target: string;
  type: 'supports' | 'contradicts';
}
