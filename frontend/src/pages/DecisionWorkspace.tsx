import { useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { createMockAgents, createMockEvents } from '@/mock/data';
import CaseHeader from '@/components/workspace/CaseHeader';
import AgentTrio from '@/components/workspace/AgentTrio';
import ConsensusPanel from '@/components/workspace/ConsensusPanel';
import DecisionInput from '@/components/workspace/DecisionInput';
import { EvidenceGraph } from '@/components/evidence';
import { Button } from '@/components/ui';

export default function DecisionWorkspace() {
  const { caseId } = useParams<{ caseId: string }>();
  const navigate = useNavigate();
  const currentCase = useCaseStore((s) => s.case);
  const loading = useCaseStore((s) => s.loading);
  const error = useCaseStore((s) => s.error);
  const initAgents = useRef(false);

  // Load mock agents/events once
  useEffect(() => {
    if (initAgents.current) return;
    initAgents.current = true;
    useAgentStore.getState().loadAgents(createMockAgents());
    useEventStore.getState().loadEvents(createMockEvents());
  }, []);

  // Fetch case when navigating to a case route
  useEffect(() => {
    if (caseId) {
      useCaseStore.getState().fetchCase(caseId);
    }
  }, [caseId]);

  const handleCreate = async (question: string) => {
    try {
      const c = await useCaseStore.getState().createCase(question);
      navigate(`/case/${c.id}`);
    } catch {
      // error already set in store
    }
  };

  if (!caseId) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <div className="w-full max-w-lg">
          <h2 className="font-mono text-lg font-semibold text-text-primary mb-4">New Decision</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const form = e.currentTarget;
              const input = form.elements.namedItem('question') as HTMLInputElement;
              if (input.value.trim()) handleCreate(input.value.trim());
            }}
          >
            <input
              name="question"
              className="w-full bg-raised border border-border-dim rounded px-3 py-2 text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:border-accent mb-3"
              placeholder="What decision should MAGI analyze?"
              autoFocus
            />
            <div className="flex gap-2">
              <Button type="submit" disabled={loading}>
                {loading ? 'Creating...' : 'Create Case'}
              </Button>
            </div>
          </form>
          {error && <p className="text-red-400 text-xs mt-2 font-mono">{error}</p>}
        </div>
      </div>
    );
  }

  if (loading || !currentCase) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <span className="font-mono text-text-muted">{loading ? 'Loading...' : 'Case not found'}</span>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <DecisionInput />
      <CaseHeader />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
