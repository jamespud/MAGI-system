import { useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { api } from '@/api/client';
import { subscribeCaseStream } from '@/api/stream';
import { mapBackendEvent } from '@/api/eventMapper';
import { ACTIVE_CASE_STATUSES, type CaseStatus } from '@/types/case';
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
  const unsubRef = useRef<(() => void) | null>(null);

  // refreshCaseData re-fetches the case + all artifacts. Called on open and on
  // run completion (via the SSE terminal callback) so the UI reflects the final
  // status/consensus/votes without a manual page refresh.
  const refreshCaseData = (id: string) => {
    useCaseStore.getState().fetchCase(id, { silent: true });
    api.getAgents(id)
      .then((snap) => useAgentStore.getState().loadAgentsFromApi(snap))
      .catch(() => {});
    api.getEvents(id)
      .then((evs) => evs
        .filter((e) => e.type !== 'CASE_STATUS_CHANGED')
        .forEach((e) => useEventStore.getState().pushEvent(mapBackendEvent(e))))
      .catch(() => {});
  };

  // Load real data + subscribe to SSE when a case is opened.
  useEffect(() => {
    if (!caseId) return;
    useEventStore.getState().clearEvents();
    refreshCaseData(caseId);
    unsubRef.current = subscribeCaseStream(caseId, () => refreshCaseData(caseId));
    return () => {
      unsubRef.current?.();
      unsubRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
    try {
      await useCaseStore.getState().runCase(caseId);
    } catch {
      // 409 or error already set in store
    }
  };

  function runButtonState(status: string): { disabled: boolean; label: string } {
    if (ACTIVE_CASE_STATUSES.includes(status as CaseStatus)) return { disabled: true, label: 'Running...' };
    if (status === 'RESOLVED') return { disabled: true, label: 'Resolved' };
    if (['FAILED','DEADLOCKED','CANCELLED','TIMED_OUT'].includes(status))
      return { disabled: false, label: 'Re-run' };
    return { disabled: false, label: 'Run Decision' };
  }

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

  const btn = runButtonState(currentCase.status);

  return (
    <div className="h-full overflow-y-auto">
      <DecisionInput />
      <div className="px-4 mb-4">
        <Button onClick={handleRun} disabled={btn.disabled}>
          {btn.label}
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
