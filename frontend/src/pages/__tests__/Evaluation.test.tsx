import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Evaluation from '../Evaluation';

const mockEvaluate = vi.fn();

vi.mock('@/api/client', () => ({
  api: { evaluateCase: (...a: unknown[]) => mockEvaluate(...a) },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Evaluation page', () => {
  it('evaluates a case and renders metrics', async () => {
    mockEvaluate.mockResolvedValue({
      tool_success_rate: 1,
      gate_failures: 0,
      total_tokens: 1234,
      first_round_consensus: true,
      consensus_round: 1,
    });

    const { getByPlaceholderText, getByRole, findByText } = render(<Evaluation />);
    fireEvent.change(getByPlaceholderText(/Case ID/), { target: { value: 'case-1' } });
    fireEvent.click(getByRole('button', { name: 'Evaluate' }));

    await waitFor(() => expect(mockEvaluate).toHaveBeenCalledWith('case-1'));
    await findByText('100%');
    expect(document.body.textContent ?? '').toContain('1,234');
    expect(document.body.textContent ?? '').toContain('First-round consensus');
  });
});
