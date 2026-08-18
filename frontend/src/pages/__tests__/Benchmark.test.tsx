import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Benchmark from '../Benchmark';

const mockBenchmark = vi.fn();
const mockGetEvalSummary = vi.fn();
const mockSeedBuiltin = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    benchmarkCases: (...a: unknown[]) => mockBenchmark(...a),
    getEvalSummary: (...a: unknown[]) => mockGetEvalSummary(...a),
    seedBuiltinBenchmark: (...a: unknown[]) => mockSeedBuiltin(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
  mockGetEvalSummary.mockResolvedValue({
    total_runs: 0, succeeded_runs: 0, failed_runs: 0,
    avg_accuracy: 0, avg_stability: 0, regression_failed_runs: 0,
    datasets: [], recent_runs: [],
  });
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

  it('renders the evaluation dashboard and seeds the built-in suite', async () => {
    mockGetEvalSummary.mockResolvedValue({
      total_runs: 3, succeeded_runs: 2, failed_runs: 1,
      avg_accuracy: 0.7, avg_stability: 0.8, regression_failed_runs: 1,
      datasets: [{ dataset_id: 'd1', name: 'MAGI Decision Sanity Suite', runs: 3, avg_accuracy: 0.7, avg_stability: 0.8 }],
      recent_runs: [{ run_id: 'r1', dataset_id: 'd1', dataset_name: 'MAGI Decision Sanity Suite', status: 'succeeded', accuracy: 0.8, stability: 0.9, regression_failed: false }],
    });
    mockSeedBuiltin.mockResolvedValue({ id: 'd1', name: 'MAGI Decision Sanity Suite', description: '', item_count: 6, created_at: 't' });

    const { getByText, findByText } = render(<Benchmark />);
    await findByText('MAGI Decision Sanity Suite');
    expect(document.body.textContent ?? '').toContain('70%');
    expect(document.body.textContent ?? '').toContain('MAGI Decision Sanity Suite');

    fireEvent.click(getByText('Seed built-in suite'));
    await waitFor(() => expect(mockSeedBuiltin).toHaveBeenCalled());
    expect(document.body.textContent ?? '').toContain('6 items');
  });
});
