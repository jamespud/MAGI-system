import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';

const { mockApi, mockSelect } = vi.hoisted(() => ({
  mockApi: {
    getEvidence: vi.fn(),
    getClaims: vi.fn(),
    getVotes: vi.fn(),
  },
  mockSelect: vi.fn(),
}));

vi.mock('react-router-dom', () => ({ useParams: () => ({ caseId: 'c1' }) }));
vi.mock('@/stores', () => ({ useUiStore: { getState: () => ({ select: mockSelect }) } }));
vi.mock('@/api/client', () => ({ api: mockApi }));

import EvidenceGraph from '../EvidenceGraph';

describe('EvidenceGraph', () => {
  beforeEach(() => {
    mockApi.getEvidence.mockReset();
    mockApi.getClaims.mockReset();
    mockApi.getVotes.mockReset();
    mockSelect.mockReset();
  });

  it('renders zoom controls + 100% when evidence exists', async () => {
    mockApi.getEvidence.mockResolvedValue([
      { id: 'EV-1', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
    ]);
    mockApi.getClaims.mockResolvedValue([
      { id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [], created_by: 'melchior' },
    ]);
    mockApi.getVotes.mockResolvedValue([
      { id: 'V-1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
    ]);

    const { getByLabelText, getByText } = render(<EvidenceGraph />);

    await waitFor(() => expect(getByLabelText('Zoom in')).toBeInTheDocument());
    expect(getByLabelText('Zoom out')).toBeInTheDocument();
    expect(getByLabelText('Reset zoom')).toBeInTheDocument();
    expect(getByText('100%')).toBeInTheDocument();
  });

  it('hides zoom controls and shows empty state when no evidence', async () => {
    mockApi.getEvidence.mockResolvedValue([]);
    mockApi.getClaims.mockResolvedValue([]);
    mockApi.getVotes.mockResolvedValue([]);

    const { queryByLabelText, getByText } = render(<EvidenceGraph />);

    await waitFor(() => expect(getByText(/No evidence yet/)).toBeInTheDocument());
    expect(queryByLabelText('Zoom in')).toBeNull();
  });

  it('links orphan evidence directly to its collector agent vote (not a mesh)', async () => {
    mockApi.getEvidence.mockResolvedValue([
      { id: 'EV-1', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
      { id: 'EV-2', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
    ]);
    mockApi.getClaims.mockResolvedValue([
      // claim has NO supports, so EV-1 and EV-2 are orphans -> link to vote
      { id: 'CL-1', text: 'claim', supports: [], contradicts: [], created_by: 'melchior' },
    ]);
    mockApi.getVotes.mockResolvedValue([
      { id: 'V-1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
    ]);

    const { container } = render(<EvidenceGraph />);

    await waitFor(() => {
      const lines = container.querySelectorAll("line[stroke]");
      // 2 orphan evidence -> vote + 1 claim -> vote = 3 links (no mesh)
      expect(lines.length).toBe(3);
    });
  });

  it('does not double-link referenced evidence (links via claim, not vote)', async () => {
    mockApi.getEvidence.mockResolvedValue([
      // EV-1 IS referenced by CL-1.supports -> links via claim, NOT to vote
      { id: 'EV-1', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
    ]);
    mockApi.getClaims.mockResolvedValue([
      { id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [], created_by: 'melchior' },
    ]);
    mockApi.getVotes.mockResolvedValue([
      { id: 'V-1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
    ]);

    const { container } = render(<EvidenceGraph />);

    await waitFor(() => {
      const lines = container.querySelectorAll("line[stroke]");
      // EV-1 -> CL-1 (supports) + CL-1 -> vote = 2 links; EV-1 NOT -> vote
      expect(lines.length).toBe(2);
    });
  });
});
