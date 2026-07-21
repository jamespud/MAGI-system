import type { Stance } from '@/lib/stance';

export type AgentId = 'melchior' | 'balthasar' | 'casper';

export const AGENT_NAMES: Record<AgentId, string> = {
  melchior: 'MELCHIOR',
  balthasar: 'BALTHASAR',
  casper: 'CASPER',
};

export const AGENT_ROLES: Record<AgentId, string> = {
  melchior: 'Scientist — Logic & Analysis',
  balthasar: 'Protector — Risk & Safety',
  casper: 'Innovator — Opportunity & Vision',
};

export const AGENT_COLORS: Record<AgentId, string> = {
  melchior: 'var(--melchior)',
  balthasar: 'var(--balthasar)',
  casper: 'var(--casper)',
};

export type AgentStatus = 'idle' | 'running' | 'waiting' | 'completed' | 'error';

export interface ToolCall {
  name: string;
  params: Record<string, string>;
  result: string | null;
  timestamp: string;
}

export interface EvidenceRef {
  id: string;
  source: string;
  reliability: number;
}

export interface ClaimRef {
  id: string;
  text: string;
  supports: string[];
  contradicts: string[];
}

export interface AgentVote {
  stance: Stance;
  confidence: number;
  reasoning: string;
  dimensions?: Record<string, number>;
}

export interface AgentSnapshot {
  agentId: AgentId;
  status: AgentStatus;
  step: number;
  maxSteps: number;
  thought: string;
  toolCalls: ToolCall[];
  evidence: EvidenceRef[];
  claims: ClaimRef[];
  vote: AgentVote | null;
}
