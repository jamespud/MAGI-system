import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api, type ApiCaseResponse } from '../client';

const MOCK_CASE: ApiCaseResponse = {
  id: 'case-001',
  question: 'Test question?',
  background: 'Test background',
  constraints: [{ label: 'Budget', value: '3 months' }],
  status: 'DRAFT',
  consensus: null,
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
