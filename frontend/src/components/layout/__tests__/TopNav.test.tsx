import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

const { mockGetStatus } = vi.hoisted(() => ({ mockGetStatus: vi.fn() }));

vi.mock('@/api/client', () => ({
  api: { getStatus: mockGetStatus },
}));

import TopNav from '@/components/layout/TopNav';

describe('TopNav', () => {
  it('renders live harness status', async () => {
    mockGetStatus.mockResolvedValue({
      model_name: 'deepseek-v4-flash',
      tokens_total: 1234,
      cost_usd: 0.25,
      runs_active: 1,
      connected: true,
    });
    render(<MemoryRouter><TopNav /></MemoryRouter>);
    await screen.findByText('deepseek-v4-flash');
    expect(screen.getByText('Running')).toBeDefined();
    expect(screen.getByText('Connected')).toBeDefined();
    expect(screen.getByText(/1,234 Tokens/)).toBeDefined();
  });

  it('shows offline state when the API is unreachable', async () => {
    mockGetStatus.mockRejectedValue(new Error('down'));
    render(<MemoryRouter><TopNav /></MemoryRouter>);
    await screen.findByText('Offline');
  });
});
