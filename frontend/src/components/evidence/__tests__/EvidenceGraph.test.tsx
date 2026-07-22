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

  it('hides orphan evidence not referenced by any claim', async () => {
    mockApi.getEvidence.mockResolvedValue([
      // EV-1 is orphan (not in any claim.supports) -> hidden; EV-2 referenced -> shown
      { id: 'EV-1', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
      { id: 'EV-2', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
    ]);
    mockApi.getClaims.mockResolvedValue([
      { id: 'CL-1', text: 'claim', supports: ['EV-2'], contradicts: [], created_by: 'melchior' },
    ]);
    mockApi.getVotes.mockResolvedValue([
      { id: 'V-1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
    ]);

    const { container } = render(<EvidenceGraph />);

    await waitFor(() => {
      // Only EV-2 -> CL-1 + CL-1 -> vote = 2 links; EV-1 hidden (no node, no link)
      const lines = container.querySelectorAll("line[stroke]");
      expect(lines.length).toBe(2);
    });
    // EV-1 label must not appear; EV-2 label must appear
    expect(container.textContent).not.toContain('EV-1');
  });

  it('dedups votes to one node per agent (latest round)', async () => {
    mockApi.getEvidence.mockResolvedValue([
      { id: 'EV-1', source: 's', observation: 'o', reliability: 0.8, collected_by: 'melchior', timestamp: 't' },
    ]);
    mockApi.getClaims.mockResolvedValue([
      { id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [], created_by: 'melchior' },
    ]);
    // two melchior votes (round 1 investigate + round 2 reconsider) -> one node
    mockApi.getVotes.mockResolvedValue([
      { id: 'V-r1', agent_code: 'melchior', stance: 'abstain', confidence: 50, reasoning: 'r', round: 1 },
      { id: 'V-r2', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 2 },
    ]);

    const { container } = render(<EvidenceGraph />);

    await waitFor(() => {
      const lines = container.querySelectorAll("line[stroke]");
      // EV-1 -> CL-1 + CL-1 -> vote = 2 links (one vote node, not two)
      expect(lines.length).toBe(2);
    });
    // latest round (r2, approve) wins
    expect(container.textContent).toContain('approve');
    expect(container.textContent).not.toContain('abstain');
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
