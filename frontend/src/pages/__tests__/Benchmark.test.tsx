import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Benchmark from '../Benchmark';

const mockBenchmark = vi.fn();

vi.mock('@/api/client', () => ({
  api: { benchmarkCases: (...a: unknown[]) => mockBenchmark(...a) },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Benchmark page', () => {
  it('runs batch benchmark and renders per-case metrics', async () => {
    mockBenchmark.mockResolvedValue({
      'case-1': { tool_success_rate: 1, gate_failures: 0, total_tokens: 500, first_round_consensus: true, consensus_round: 1 },
      'case-2': { tool_success_rate: 0.5, gate_failures: 2, total_tokens: 900, first_round_consensus: false, consensus_round: 2 },
    });

    const { getByPlaceholderText, getByText, findByText } = render(<Benchmark />);
    fireEvent.change(getByPlaceholderText(/One case ID per line/), { target: { value: 'case-1\ncase-2' } });
    fireEvent.click(getByText('Run benchmark'));

    await waitFor(() => expect(mockBenchmark).toHaveBeenCalledWith(['case-1', 'case-2']));
    await findByText('case-1');
    const body = document.body.textContent ?? '';
    expect(body).toContain('500');
    expect(body).toContain('900');
  });
});
