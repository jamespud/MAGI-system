import { useEffect, useRef } from 'react';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { createMockCase, createMockAgents, createMockEvents, createMockCaseList } from '@/mock/data';
import CaseHeader from '@/components/workspace/CaseHeader';
import AgentTrio from '@/components/workspace/AgentTrio';
import ConsensusPanel from '@/components/workspace/ConsensusPanel';
import { EvidenceGraph } from '@/components/evidence';

export default function DecisionWorkspace() {
  const currentCase = useCaseStore((s) => s.case);
  const loaded = useRef(false);

  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;

    useCaseStore.getState().loadCase(createMockCase());
    useCaseStore.getState().loadCaseList(createMockCaseList());
    useAgentStore.getState().loadAgents(createMockAgents());
    useEventStore.getState().loadEvents(createMockEvents());
  }, []);

  if (!currentCase) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <span className="font-mono text-text-muted">Loading...</span>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <CaseHeader />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
