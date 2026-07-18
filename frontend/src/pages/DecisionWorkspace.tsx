import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { createMockCase, createMockAgents, createMockEvents, createMockCaseList } from '@/mock/data';
import CaseHeader from '@/components/workspace/CaseHeader';
import AgentTrio from '@/components/workspace/AgentTrio';
import ConsensusPanel from '@/components/workspace/ConsensusPanel';
import DecisionInput from '@/components/workspace/DecisionInput';
import { EvidenceGraph } from '@/components/evidence';

export default function DecisionWorkspace() {
  const { caseId } = useParams<{ caseId: string }>();
  const loadCase = useCaseStore((s) => s.loadCase);
  const loadCaseList = useCaseStore((s) => s.loadCaseList);
  const loadAgents = useAgentStore((s) => s.loadAgents);
  const loadEvents = useEventStore((s) => s.loadEvents);
  const currentCase = useCaseStore((s) => s.case);

  useEffect(() => {
    const caseData = createMockCase();
    const agents = createMockAgents();
    const events = createMockEvents();
    const caseList = createMockCaseList();

    loadCase(caseData);
    loadAgents(agents);
    loadEvents(events);
    loadCaseList(caseList);
  }, [caseId, loadCase, loadAgents, loadEvents, loadCaseList]);

  if (!currentCase) {
    return (
      <div className="flex h-full items-center justify-center">
        <span className="font-mono text-text-muted animate-pulse-glow">Loading case data...</span>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <CaseHeader />
      <DecisionInput />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
