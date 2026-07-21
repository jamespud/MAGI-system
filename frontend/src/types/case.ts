export type CaseStatus =
  | 'DRAFT'
  | 'NORMALIZING'
  | 'CONTEXT_BUILDING'
  | 'RETRIEVING_MEMORY'
  | 'INVESTIGATING'
  | 'EVIDENCE_GATING'
  | 'COLLECTING_VOTES'
  | 'CONSENSUS_CHECK'
  | 'DEBATING'
  | 'REFLECTING'
  | 'REVOTING'
  | 'RESOLVING'
  | 'GENERATING_REPORT'
  | 'SAVING_MEMORY'
  | 'EVALUATING'
  | 'RESOLVED'
  | 'DEADLOCKED'
  | 'FAILED'
  | 'CANCELLED'
  | 'TIMED_OUT'
  | 'INSUFFICIENT_EVIDENCE'
  | 'MEMORY_INDEXED';

export const CASE_STATUS_LABELS: Record<CaseStatus, string> = {
  DRAFT: 'Draft',
  NORMALIZING: 'Normalizing',
  CONTEXT_BUILDING: 'Building Context',
  RETRIEVING_MEMORY: 'Retrieving Memory',
  INVESTIGATING: 'Investigating',
  EVIDENCE_GATING: 'Evidence Gate',
  COLLECTING_VOTES: 'Collecting Votes',
  CONSENSUS_CHECK: 'Consensus Check',
  DEBATING: 'Debating',
  REFLECTING: 'Reflecting',
  REVOTING: 'Re-voting',
  RESOLVING: 'Resolving',
  GENERATING_REPORT: 'Generating Report',
  SAVING_MEMORY: 'Saving Memory',
  EVALUATING: 'Evaluating',
  RESOLVED: 'Resolved',
  DEADLOCKED: 'Deadlocked',
  FAILED: 'Failed',
  CANCELLED: 'Cancelled',
  TIMED_OUT: 'Timed Out',
  INSUFFICIENT_EVIDENCE: 'Insufficient Evidence',
  MEMORY_INDEXED: 'Memory Indexed',
};

export interface Case {
  id: string;
  question: string;
  background: string;
  constraints: Constraint[];
  status: CaseStatus;
  round: number;
  consensus: ConsensusState | null;
  confidence: number;
  finalDecision: string;
  createdAt: string;
  updatedAt: string;
}

export interface CaseSummary {
  id: string;
  question: string;
  status: CaseStatus;
  round: number;
  createdAt: string;
  pinned: boolean;
}

export interface Constraint {
  label: string;
  value: string;
}

export interface ConsensusState {
  approve: number;
  reject: number;
  abstain: number;
  majority: 'Approve' | 'Reject' | 'Tie';
  needReflection: boolean;
}
