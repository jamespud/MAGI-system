import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Replay from '../Replay';

const mockGetEvents = vi.fn();

vi.mock('@/api/client', () => ({
  api: { getEvents: (...a: unknown[]) => mockGetEvents(...a) },
}));

beforeEach(() => {
  vi.restoreAllMocks();
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
});
