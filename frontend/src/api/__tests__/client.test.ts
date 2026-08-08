import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api, type ApiCaseResponse } from '../client';

const MOCK_CASE: ApiCaseResponse = {
  id: 'case-001',
  question: 'Test question?',
  background: 'Test background',
  constraints: [{ label: 'Budget', value: '3 months' }],
  status: 'DRAFT',
  consensus: null,
  final_decision: '',
  confidence: 0,
  round: 0,
  created_at: '2026-07-19T10:00:00Z',
  updated_at: '2026-07-19T10:00:00Z',
};

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('api.getCases', () => {
  it('fetches cases from /api/v1/cases', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([MOCK_CASE]),
    } as Response);

    const result = await api.getCases();

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases', {
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result).toEqual([MOCK_CASE]);
  });

  it('throws on non-ok response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'server error' }),
    } as Response);

    await expect(api.getCases()).rejects.toThrow('server error');
  });
});

describe('api.getCase', () => {
  it('fetches single case by id', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(MOCK_CASE),
    } as Response);

    const result = await api.getCase('case-001');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001', {
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result).toEqual(MOCK_CASE);
  });
});

describe('api.createCase', () => {
  it('posts case with question and background', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(MOCK_CASE),
    } as Response);

    const result = await api.createCase('Test question?', 'Test background');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ question: 'Test question?', background: 'Test background' }),
    });
    expect(result).toEqual(MOCK_CASE);
  });
});

describe('api.runCase', () => {
  it('posts to /cases/:id/run and returns 202 body', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 202,
      json: () => Promise.resolve({ id: 'case-001', status: 'INVESTIGATING' }),
    } as Response);

    const result = await api.runCase('case-001');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result).toEqual({ id: 'case-001', status: 'INVESTIGATING' });
  });
});

describe('api.cancelCase', () => {
  it('posts to /cases/:id/cancel', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ id: 'case-001', status: 'CANCELLED' }),
    } as Response);

    const result = await api.cancelCase('case-001');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/cancel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result.status).toBe('CANCELLED');
  });
});

describe('api.getEvidence', () => {
  it('fetches evidence array for a case', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([
        { id: 'EV-1', source: 'local', observation: 'obs', reliability: 0.9, collected_by: 'melchior', timestamp: 't' },
      ]),
    } as Response);

    const result = await api.getEvidence('case-001');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/evidence', {
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result[0].collected_by).toBe('melchior');
  });
});

describe('api.getAgents', () => {
  it('fetches agent snapshot map', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        melchior: {
          agent_code: 'melchior', status: 'completed', round: 1, step: 2,
          tool_calls: [], evidence: [], claims: [],
        },
      }),
    } as Response);

    const result = await api.getAgents('case-001');

    expect(result.melchior.step).toBe(2);
  });
});

describe('api.getEvents', () => {
  it('fetches events with id + message + payload', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([
        { id: 'e1', type: 'VOTE_SUBMITTED', message: 'Votes submitted', payload: { round: 1 }, timestamp: 't' },
      ]),
    } as Response);

    const result = await api.getEvents('case-001');

    expect(result[0].id).toBe('e1');
    expect(result[0].message).toBe('Votes submitted');
  });
});

describe('api.getCases wrapper', () => {
  it('unwraps {cases: [...]} envelope', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ cases: [{ ...MOCK_CASE, final_decision: 'approve' }] }),
    } as Response);

    const result = await api.getCases();

    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('case-001');
  });
});

describe('api.datasets', () => {
  it('creates a dataset', async () => {
    const ds = { id: 'd1', name: 'eval', description: '', item_count: 0, created_at: 'x' };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(ds),
    } as Response);
    await expect(api.createDataset('eval')).resolves.toEqual(ds);
    expect(fetch).toHaveBeenCalledWith('/api/v1/datasets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'eval', description: undefined }),
    });
  });

  it('starts a dataset run', async () => {
    const run = { id: 'r1', dataset_id: 'd1', status: 'queued', total: 2, matched: 0, accuracy: 0, weighted_accuracy: 0, started_at: 'x' };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(run),
    } as Response);
    await expect(api.startDatasetRun('d1')).resolves.toEqual(run);
    expect(fetch).toHaveBeenCalledWith('/api/v1/datasets/d1/runs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
  });

  it('fetches benchmark run detail', async () => {
    const detail = { run: { id: 'r1', dataset_id: 'd1', status: 'succeeded', total: 2, matched: 1, accuracy: 0.5, weighted_accuracy: 0.5, started_at: 'x' }, results: [] };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(detail),
    } as Response);
    await expect(api.getBenchmarkRun('r1')).resolves.toEqual(detail);
    expect(fetch).toHaveBeenCalledWith('/api/v1/benchmarks/r1', {
      headers: { 'Content-Type': 'application/json' },
    });
  });
});

describe('api.searchMemory', () => {
  it('queries memory with encoded query and limit', async () => {
    const results = { results: [{ CaseID: 'case-1', QuestionSummary: 'stack choice', ContextSummary: '', Resolution: 'approve', ProjectionVersion: 1 }] };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(results),
    } as Response);
    await expect(api.searchMemory('stack choice', 5)).resolves.toEqual(results);
    expect(fetch).toHaveBeenCalledWith('/api/v1/memory?q=stack%20choice&limit=5', {
      headers: { 'Content-Type': 'application/json' },
    });
  });
});
