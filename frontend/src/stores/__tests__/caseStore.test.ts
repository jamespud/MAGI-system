import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useCaseStore } from '../caseStore';
import { api } from '@/api/client';
import type { Case } from '@/types/case';

vi.mock('@/api/client', () => ({
  api: {
    getCases: vi.fn(),
    getCase: vi.fn(),
    createCase: vi.fn(),
    forkCase: vi.fn(),
    runCase: vi.fn(),
    cancelCase: vi.fn(),
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
  finalDecision: '',
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
    useCaseStore.getState().updateCaseStatus('test-1', 'INVESTIGATING', 1);
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
      { id: 'c1', question: 'Q1?', background: '', constraints: [], status: 'DRAFT', consensus: null, confidence: 0, round: 1, final_decision: '', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      { id: 'c2', question: 'Q2?', background: '', constraints: [], status: 'RESOLVED', consensus: null, confidence: 0, round: 2, final_decision: 'approve', created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z' },
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

  it('fetchCase loads a single case from API', async () => {
    const apiCase = {
      id: 'case-001', question: 'Rust?', background: 'Java team',
      constraints: [{ label: 'Budget', value: '3m' }],
      status: 'INVESTIGATING', consensus: null, confidence: 0, round: 1,
      final_decision: 'approve',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    vi.mocked(api.getCase).mockResolvedValueOnce(apiCase);

    await useCaseStore.getState().fetchCase('case-001');

    const c = useCaseStore.getState().case;
    expect(c?.id).toBe('case-001');
    expect(c?.question).toBe('Rust?');
    expect(c?.background).toBe('Java team');
    expect(c?.constraints).toEqual([{ label: 'Budget', value: '3m' }]);
    expect(c?.status).toBe('INVESTIGATING');
    expect(c?.finalDecision).toBe('approve');
    expect(c?.createdAt).toBe('2026-01-01T00:00:00Z');
    expect(useCaseStore.getState().loading).toBe(false);
  });

  it('createCase posts and returns API response', async () => {
    const apiCase = {
      id: 'case-new', question: 'New Q?', background: '',
      constraints: [], status: 'DRAFT', consensus: null, confidence: 0, round: 0, final_decision: '',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    vi.mocked(api.createCase).mockResolvedValueOnce(apiCase);

    const result = await useCaseStore.getState().createCase('New Q?');

    expect(result.id).toBe('case-new');
    expect(api.createCase).toHaveBeenCalledWith('New Q?', undefined);
    expect(useCaseStore.getState().loading).toBe(false);
  });
});


  it('forkCase creates and upserts the forked case with its parent link', async () => {
    const apiCase = {
      id: 'case-fork', question: 'Q?', background: '', constraints: [], parent_case_id: 'case-001',
      status: 'NORMALIZING', consensus: null, confidence: 0, round: 0, final_decision: '',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    vi.mocked(api.forkCase).mockResolvedValueOnce(apiCase);

    const result = await useCaseStore.getState().forkCase('case-001');

    expect(api.forkCase).toHaveBeenCalledWith('case-001');
    expect(result.id).toBe('case-fork');
    expect(useCaseStore.getState().cases.find((c) => c.id === 'case-fork')?.parentCaseId).toBe('case-001');
  });
describe('caseStore.runCase', () => {
  it('posts run and updates case status from response', async () => {
    useCaseStore.getState().loadCase(mockCase);
    vi.mocked(api.runCase).mockResolvedValueOnce({ id: 'test-1', status: 'INVESTIGATING' });

    await useCaseStore.getState().runCase('test-1');

    expect(api.runCase).toHaveBeenCalledWith('test-1');
    expect(useCaseStore.getState().case?.status).toBe('INVESTIGATING');
    expect(useCaseStore.getState().loading).toBe(false);
  });

  it('sets error and rethrows on failure', async () => {
    useCaseStore.getState().loadCase(mockCase);
    vi.mocked(api.runCase).mockRejectedValueOnce(new Error('already running'));

    await expect(useCaseStore.getState().runCase('test-1')).rejects.toThrow('already running');
    expect(useCaseStore.getState().error).toBe('already running');
  });
});

describe('caseStore.cancelCase', () => {
  it('posts cancel without throwing', async () => {
    vi.mocked(api.cancelCase).mockResolvedValueOnce({ id: 'test-1', status: 'CANCELLED' });

    await useCaseStore.getState().cancelCase('test-1');

    expect(api.cancelCase).toHaveBeenCalledWith('test-1');
  });

  it('sets error on failure', async () => {
    vi.mocked(api.cancelCase).mockRejectedValueOnce(new Error('no active run'));
    await useCaseStore.getState().cancelCase('test-1');
    expect(useCaseStore.getState().error).toBe('no active run');
  });
});

describe('caseStore sidebar list sync', () => {
  beforeEach(() => {
    useCaseStore.setState({ case: null, cases: [], loading: false, error: null });
  });

  it('runCase patches the list entry so Running appears without refresh', async () => {
    useCaseStore.getState().loadCaseList([
      { id: 'test-1', question: 'Test question?', status: 'DRAFT', round: 1, createdAt: '2026-01-01T00:00:00Z', pinned: false },
    ]);
    useCaseStore.getState().loadCase(mockCase);
    vi.mocked(api.runCase).mockResolvedValueOnce({ id: 'test-1', status: 'INVESTIGATING' });

    await useCaseStore.getState().runCase('test-1');

    expect(useCaseStore.getState().cases.find((c) => c.id === 'test-1')?.status).toBe('INVESTIGATING');
  });

  it('createCase adds the new case to the list', async () => {
    const apiCase = {
      id: 'case-new', question: 'New Q?', background: '',
      constraints: [], status: 'DRAFT', consensus: null, confidence: 0, round: 0, final_decision: '',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    vi.mocked(api.createCase).mockResolvedValueOnce(apiCase);

    await useCaseStore.getState().createCase('New Q?');

    expect(useCaseStore.getState().cases.find((c) => c.id === 'case-new')?.status).toBe('DRAFT');
  });

  it('fetchCase upserts the list entry with latest status', async () => {
    useCaseStore.getState().loadCaseList([
      { id: 'case-001', question: 'Rust?', status: 'DRAFT', round: 1, createdAt: '2026-01-01T00:00:00Z', pinned: false },
    ]);
    const apiCase = {
      id: 'case-001', question: 'Rust?', background: 'Java team',
      constraints: [], status: 'RESOLVED', consensus: null, confidence: 0, round: 2,
      final_decision: 'approve',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    vi.mocked(api.getCase).mockResolvedValueOnce(apiCase);

    await useCaseStore.getState().fetchCase('case-001');

    expect(useCaseStore.getState().cases.find((c) => c.id === 'case-001')?.status).toBe('RESOLVED');
    expect(useCaseStore.getState().cases).toHaveLength(1);
  });

  it('updateCaseStatus patches the list entry too', () => {
    useCaseStore.getState().loadCase(mockCase);
    useCaseStore.getState().loadCaseList([
      { id: 'test-1', question: 'Test question?', status: 'DRAFT', round: 1, createdAt: '', pinned: false },
    ]);
    useCaseStore.getState().updateCaseStatus('test-1', 'DEBATING', 2);
    expect(useCaseStore.getState().cases.find((c) => c.id === 'test-1')?.status).toBe('DEBATING');
  });
});
