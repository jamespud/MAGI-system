import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, findByText } from '@testing-library/react';
import Settings from '../Settings';

const mockGetVersion = vi.fn();
const mockGetReady = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    getVersion: (...a: unknown[]) => mockGetVersion(...a),
    getReady: (...a: unknown[]) => mockGetReady(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Settings page', () => {
  it('renders version and readiness', async () => {
    mockGetVersion.mockResolvedValue({ version: '2.0.0' });
    mockGetReady.mockResolvedValue({ status: 'ready' });

    const { container } = render(<Settings />);
    await findByText(container, '2.0.0');
    expect(container.textContent ?? '').toContain('ready');
  });
});
