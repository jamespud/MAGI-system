import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Replay from '../Replay';

const mockGetEvents = vi.fn();
const mockGetTrace = vi.fn();
const mockGetTaskTree = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    getEvents: (...a: unknown[]) => mockGetEvents(...a),
    getTrace: (...a: unknown[]) => mockGetTrace(...a),
    getTaskTree: (...a: unknown[]) => mockGetTaskTree(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
  mockGetEvents.mockReset();
  mockGetTrace.mockReset();
  mockGetTaskTree.mockReset();
  mockGetTaskTree.mockResolvedValue({ nodes: [] });
});

describe('Replay page', () => {
  it('loads and renders a case event timeline', async () => {
    mockGetEvents.mockResolvedValue([
      { id: 'e1', type: 'CASE_CREATED', message: 'Case created', timestamp: '2026-08-01T10:00:00Z' },
      { id: 'e2', type: 'VOTE_SUBMITTED', run_id: 'run-1', message: 'Vote submitted', timestamp: '2026-08-01T10:01:00Z' },
    ]);

    const { getByPlaceholderText, getByRole, findByText } = render(<Replay />);
    fireEvent.change(getByPlaceholderText(/Case ID/), { target: { value: 'case-1' } });
    fireEvent.click(getByRole('button', { name: 'Replay' }));

    await waitFor(() => expect(mockGetEvents).toHaveBeenCalledWith('case-1'));
    await findByText('Vote submitted');
    expect(document.body.textContent ?? '').toContain('run-1');
  });

  it('loads and renders the trace lane view', async () => {
    mockGetTrace.mockResolvedValue([
      { id: 't1', type: 'AGENT_STEP', run_id: 'run-1', agent_code: 'melchior', message: 'Gather evidence', timestamp: '2026-08-01T10:00:00Z' },
      { id: 't2', type: 'VOTE_SUBMITTED', run_id: 'run-1', agent_code: 'casper', message: 'Vote submitted', timestamp: '2026-08-01T10:01:00Z' },
      { id: 't3', type: 'ERROR', run_id: 'run-1', message: 'Model call failed', timestamp: '2026-08-01T10:02:00Z' },
    ]);

    const { getByPlaceholderText, getByRole, findByText, getByLabelText } = render(<Replay />);
    fireEvent.change(getByPlaceholderText(/Case ID/), { target: { value: 'case-1' } });
    fireEvent.click(getByRole('button', { name: 'Trace' }));

    await waitFor(() => expect(mockGetTrace).toHaveBeenCalledWith('case-1'));
    await findByText(/run-1 · melchior, casper/);
    expect(document.body.textContent ?? '').toContain('Errors');
    expect(document.body.textContent ?? '').toContain('3');

    fireEvent.click(getByLabelText('AGENT_STEP: Gather evidence'));
    await findByText('Gather evidence');
  });
});
