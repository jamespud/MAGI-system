import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  api,
  setApiKey,
  clearApiKey,
  getApiKey,
  AUTH_STORAGE_KEY,
  UNAUTHORIZED_EVENT,
  type ApiCaseResponse,
} from '../client';

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
  clearApiKey(); // ensure no API key leaks into header assertions
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

describe('api.getTrace', () => {
  it('fetches the execution trace for a case', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([
        { id: 't1', type: 'AGENT_STEP', agent_code: 'melchior', run_id: 'run-1', message: 'step 1', timestamp: 't' },
      ]),
    } as Response);

    const result = await api.getTrace('case-001');

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/trace', {
      headers: { 'Content-Type': 'application/json' },
    });
    expect(result[0].id).toBe('t1');
    expect(result[0].type).toBe('AGENT_STEP');
  });
});

describe('api.pauseCase and resumeCase', () => {
  it('posts pause and resume for a case', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ id: 'case-001', status: 'PAUSED' }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ id: 'case-001', status: 'DRAFT' }),
      } as Response);

    const paused = await api.pauseCase('case-001');
    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/pause', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(paused.status).toBe('PAUSED');

    const resumed = await api.resumeCase('case-001');
    expect(fetch).toHaveBeenCalledWith('/api/v1/cases/case-001/resume', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(resumed.status).toBe('DRAFT');
  });
});

describe('api.seedBuiltinBenchmark and getEvalSummary', () => {
  it('seeds the built-in suite and fetches the aggregate summary', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ id: 'd1', name: 'MAGI Decision Sanity Suite', item_count: 6, description: '', created_at: 't' }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          total_runs: 2, succeeded_runs: 2, failed_runs: 0,
          avg_accuracy: 0.8, avg_stability: 0.9, regression_failed_runs: 0,
          datasets: [], recent_runs: [],
        }),
      } as Response);

    const dataset = await api.seedBuiltinBenchmark();
    expect(fetch).toHaveBeenCalledWith('/api/v1/admin/benchmarks/seed', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(dataset.name).toBe('MAGI Decision Sanity Suite');

    const summary = await api.getEvalSummary();
    expect(fetch).toHaveBeenCalledWith('/api/v1/admin/eval/summary', {
      headers: { 'Content-Type': 'application/json' },
    });
    expect(summary.avg_accuracy).toBe(0.8);
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


describe('api memory governance', () => {
  it('updates a memory projection', async () => {
    const memory = { CaseID: 'case-1', QuestionSummary: 'q', ContextSummary: '', Resolution: 'approve', Annotation: 'trusted', Tags: ['ops'], ProjectionVersion: 1 };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(memory),
    } as Response);
    await expect(api.updateMemory('case 1', { annotation: 'trusted', tags: ['ops'] })).resolves.toEqual(memory);
    expect(fetch).toHaveBeenCalledWith('/api/v1/memory/case%201', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ annotation: 'trusted', tags: ['ops'] }),
    });
  });

  it('deletes a memory projection', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({ ok: true, status: 204 } as Response);
    await expect(api.deleteMemory('case-1')).resolves.toBeUndefined();
    expect(fetch).toHaveBeenCalledWith('/api/v1/memory/case-1', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
    });
  });
});

describe('api auth channel (P0: D1)', () => {
  it('injects X-API-Key header when a key is stored', async () => {
    setApiKey('sk-test');
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([MOCK_CASE]),
    } as Response);

    await api.getCases();

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases', {
      headers: { 'Content-Type': 'application/json', 'X-API-Key': 'sk-test' },
    });
  });

  it('does not send X-API-Key when no key is stored', async () => {
    clearApiKey();
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([]),
    } as Response);

    await api.getCases();

    expect(fetch).toHaveBeenCalledWith('/api/v1/cases', {
      headers: { 'Content-Type': 'application/json' },
    });
  });

  it('clears the stored key and dispatches UNAUTHORIZED_EVENT on 401', async () => {
    setApiKey('sk-expired');
    const dispatched: string[] = [];
    const onEvent = (e: Event) => dispatched.push(e.type);
    window.addEventListener(UNAUTHORIZED_EVENT, onEvent);
    try {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ error: 'unauthorized' }),
      } as Response);

      await expect(api.getCases()).rejects.toThrow('unauthorized');
    } finally {
      window.removeEventListener(UNAUTHORIZED_EVENT, onEvent);
    }
    expect(getApiKey()).toBe('');
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
    expect(dispatched).toContain(UNAUTHORIZED_EVENT);
  });

  it('verifyAuth returns true on 200 and false on 401', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ status: 'ok' }) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response);

    expect(await api.verifyAuth()).toBe(true);
    expect(await api.verifyAuth()).toBe(false);
    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/status', {
      headers: { 'Content-Type': 'application/json' },
    });
  });
});

describe('api conversations', () => {
  it('starts or continues a conversation through /assistant', async () => {
    const response = { ...MOCK_CASE, conversation_id: 'conv-001' };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 202,
      json: () => Promise.resolve(response),
    } as Response);

    await expect(api.askAssistant('Follow up?', 'conv-001', 'budget')).resolves.toEqual(response);
    expect(fetch).toHaveBeenCalledWith('/api/v1/assistant', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: 'Follow up?', conversation_id: 'conv-001', background: 'budget' }),
    });
  });

  it('lists and fetches conversation threads', async () => {
    const conversation = { id: 'conv-001', title: 'Launch', created_at: '2026-08-18T01:00:00Z', updated_at: '2026-08-18T02:00:00Z' };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ conversations: [conversation] }),
    } as Response);
    await expect(api.listConversations(20, 10)).resolves.toEqual({ conversations: [conversation] });
    expect(fetch).toHaveBeenCalledWith('/api/v1/conversations?limit=20&offset=10', {
      headers: { 'Content-Type': 'application/json' },
    });

    const detail = { conversation, messages: [] };
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(detail),
    } as Response);
    await expect(api.getConversation('conv 001')).resolves.toEqual(detail);
    expect(fetch).toHaveBeenCalledWith('/api/v1/conversations/conv%20001', {
      headers: { 'Content-Type': 'application/json' },
    });
  });

  it('handles 204 from conversation deletion', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 204,
      json: () => Promise.resolve(null),
    } as Response);

    await expect(api.deleteConversation('conv-001')).resolves.toBeUndefined();
    expect(fetch).toHaveBeenCalledWith('/api/v1/conversations/conv-001', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
    });
  });
});
