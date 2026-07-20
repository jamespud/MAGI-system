import { useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { api } from '@/api/client';
import { subscribeCaseStream } from '@/api/stream';
import { mapBackendEvent } from '@/api/eventMapper';
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
  const [running, setRunning] = useState(false);
  const unsubRef = useRef<(() => void) | null>(null);

  // Load real data + subscribe to SSE when a case is opened.
  useEffect(() => {
    if (!caseId) return;
    useCaseStore.getState().fetchCase(caseId);
    useEventStore.getState().clearEvents();
    api.getAgents(caseId)
      .then((snap) => useAgentStore.getState().loadAgentsFromApi(snap))
      .catch(() => {});
    api.getEvents(caseId)
      .then((evs) => evs.forEach((e) => useEventStore.getState().pushEvent(mapBackendEvent(e))))
      .catch(() => {});
    unsubRef.current = subscribeCaseStream(caseId);
    return () => {
      unsubRef.current?.();
      unsubRef.current = null;
    };
  }, [caseId]);

  const handleCreate = async (question: string) => {
    try {
      const c = await useCaseStore.getState().createCase(question);
      navigate(`/case/${c.id}`);
    } catch {
      // error already set in store
    }
  };

  const handleRun = async () => {
    if (!caseId) return;
    setRunning(true);
    try {
      await useCaseStore.getState().runCase(caseId);
    } catch {
      // 409 or error already set in store
    } finally {
      setRunning(false);
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

  const resolved = currentCase.status === 'RESOLVED';

  return (
    <div className="h-full overflow-y-auto">
      <DecisionInput />
      <div className="px-4 mb-4">
        <Button onClick={handleRun} disabled={running || resolved}>
          {running ? 'Running...' : resolved ? 'Resolved' : 'Run Decision'}
        </Button>
        {error && <span className="ml-3 text-red-400 text-xs font-mono">{error}</span>}
      </div>
      <CaseHeader />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
