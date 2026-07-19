import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useCaseStore } from '../caseStore';
import { api } from '@/api/client';
import type { Case } from '@/types/case';

vi.mock('@/api/client', () => ({
  api: {
    getCases: vi.fn(),
    getCase: vi.fn(),
    createCase: vi.fn(),
  },
}));

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

  it('fetchCases populates case list from API', async () => {
    const mockApiCases = [
      { id: 'c1', question: 'Q1?', background: '', constraints: [], status: 'DRAFT', consensus: null, confidence: 0, round: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      { id: 'c2', question: 'Q2?', background: '', constraints: [], status: 'RESOLVED', consensus: null, confidence: 0, round: 2, created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z' },
    ];
    vi.mocked(api.getCases).mockResolvedValueOnce(mockApiCases);

    await useCaseStore.getState().fetchCases();

    expect(useCaseStore.getState().cases).toHaveLength(2);
    expect(useCaseStore.getState().cases[0].id).toBe('c1');
    expect(useCaseStore.getState().cases[0].status).toBe('DRAFT');
    expect(useCaseStore.getState().cases[1].id).toBe('c2');
    expect(useCaseStore.getState().cases[1].status).toBe('RESOLVED');
    expect(useCaseStore.getState().loading).toBe(false);
  });

  it('fetchCases sets error on failure', async () => {
    vi.mocked(api.getCases).mockRejectedValueOnce(new Error('network error'));

    await useCaseStore.getState().fetchCases();

    expect(useCaseStore.getState().error).toBe('network error');
    expect(useCaseStore.getState().loading).toBe(false);
  });
});
