import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import Tools from '../Tools';

const mockListTools = vi.fn();

vi.mock('@/api/client', () => ({
  api: { listTools: (...a: unknown[]) => mockListTools(...a) },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Tools page', () => {
  it('renders available tools', async () => {
    mockListTools.mockResolvedValue([
      { name: 'web_search', desc: 'Search the web for up-to-date information.' },
      { name: 'code_runner', desc: 'Run Python or JavaScript code in the sandbox.' },
    ]);

    const { findByText } = render(<Tools />);
    await findByText('web_search');
    expect(document.body.textContent ?? '').toContain('Run Python or JavaScript code');
  });
});
