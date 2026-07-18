import { describe, it, expect, beforeEach } from 'vitest';
import { useCaseStore } from '../caseStore';
import type { Case } from '@/types/case';

const mockCase: Case = {
  id: 'test-1',
  question: 'Test question?',
  background: 'Test background',
  constraints: [],
  status: 'DRAFT',
  round: 1,
  consensus: null,
  confidence: 0,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

describe('caseStore', () => {
  beforeEach(() => {
    useCaseStore.setState({ case: null, cases: [], loading: false, error: null });
  });

  it('loads a case', () => {
    useCaseStore.getState().loadCase(mockCase);
    expect(useCaseStore.getState().case?.id).toBe('test-1');
    expect(useCaseStore.getState().loading).toBe(false);
  });

  it('loads case list', () => {
    useCaseStore.getState().loadCaseList([
      { id: '1', question: 'Q1', status: 'DRAFT', round: 1, createdAt: '', pinned: false },
    ]);
    expect(useCaseStore.getState().cases).toHaveLength(1);
  });

  it('updates case status', () => {
    useCaseStore.getState().loadCase(mockCase);
    useCaseStore.getState().updateCaseStatus('INVESTIGATING', 1);
    expect(useCaseStore.getState().case?.status).toBe('INVESTIGATING');
  });

  it('updates consensus', () => {
    useCaseStore.getState().loadCase(mockCase);
    const consensus = { approve: 2, reject: 1, abstain: 0, majority: 'Approve' as const, needReflection: false };
    useCaseStore.getState().updateConsensus(consensus, 81);
    expect(useCaseStore.getState().case?.consensus?.approve).toBe(2);
    expect(useCaseStore.getState().case?.confidence).toBe(81);
  });
});
