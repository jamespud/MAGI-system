import { useUiStore, useAgentStore, useEventStore } from '@/stores';
import { MonoText, AgentAvatar } from '@/components/shared';
import { Globe, Link2, Shield, Clock, FileText } from 'lucide-react';
import { createMockEvidence } from '@/mock/data';

const MOCK_EVIDENCE_MAP = new Map(createMockEvidence().map((e) => [e.id, e]));

export default function RightInspector() {
  const selected = useUiStore((s) => s.selected);
  const agents = useAgentStore((s) => s.agents);
  const events = useEventStore((s) => s.events);

  if (!selected) {
    return (
      <aside className="w-80 shrink-0 border-l border-border-dim bg-base p-4 flex items-center justify-center">
        <div className="text-center">
          <FileText size={24} className="text-text-muted mx-auto mb-2" />
          <MonoText muted>Inspector</MonoText>
          <p className="text-xs text-text-muted mt-1">Select an object to inspect</p>
        </div>
      </aside>
    );
  }

  const renderEvidenceDetail = () => {
    const ev = MOCK_EVIDENCE_MAP.get(selected.id);
    if (!ev) return <MonoText muted>Evidence {selected.id} not found</MonoText>;
    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Source</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Globe size={14} className="text-text-muted" />
            <span className="text-sm text-text-primary">{ev.source}</span>
          </div>
        </div>
        {ev.url && (
          <div>
            <MonoText size="sm" muted>URL</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Link2 size={14} className="text-text-muted" />
              <span className="text-xs text-accent truncate block">{ev.url}</span>
            </div>
          </div>
        )}
        <div>
          <MonoText size="sm" muted>Observation</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed">{ev.observation}</p>
        </div>
        <div className="flex items-center gap-4">
          <div>
            <MonoText size="sm" muted>Reliability</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Shield size={14} className="text-accent" />
              <span className="font-mono text-sm font-semibold text-accent">{ev.reliability.toFixed(2)}</span>
            </div>
          </div>
          <div>
            <MonoText size="sm" muted>Collected By</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={ev.collectedBy} size={18} />
              <span className="text-sm text-text-secondary">{ev.collectedBy}</span>
            </div>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Timestamp</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Clock size={14} className="text-text-muted" />
            <span className="font-mono text-xs text-text-muted">{ev.timestamp}</span>
          </div>
        </div>
      </div>
    );
  };

  const renderVoteDetail = () => {
    const agentVotes = Object.entries(agents)
      .filter(([, a]) => a?.vote?.stance)
      .map(([id, a]) => ({ agentId: id, ...a!.vote! }));

    const vote = agentVotes.find((v) => v.agentId === selected.id) || agentVotes[0];
    if (!vote) return <MonoText muted>No vote data available</MonoText>;

    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Stance</MonoText>
          <div className="mt-1">
            <span className={`font-mono text-base font-bold ${vote.stance === 'Approve' ? 'text-accent' : 'text-error'}`}>
              {vote.stance.toUpperCase()}
            </span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Confidence</MonoText>
          <div className="mt-1">
            <span className="font-mono text-lg font-semibold text-text-primary">{vote.confidence}%</span>
          </div>
        </div>
        {vote.dimensions && (
          <div>
            <MonoText size="sm" muted>Utility Dimensions</MonoText>
            <div className="mt-2 space-y-2">
              {Object.entries(vote.dimensions).map(([dim, val]) => (
                <div key={dim}>
                  <div className="flex justify-between text-xs mb-1">
                    <span className="text-text-secondary">{dim}</span>
                    <span className="font-mono text-text-primary">{val}</span>
                  </div>
                  <div className="h-1 bg-border-dim rounded-full overflow-hidden">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${val}%` }} />
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
        <div>
          <MonoText size="sm" muted>Reasoning</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed">{vote.reasoning}</p>
        </div>
      </div>
    );
  };

  const renderAgentDetail = () => {
    const agent = agents[selected.id as keyof typeof agents];
    if (!agent) return <MonoText muted>No agent data</MonoText>;
    return (
      <div className="space-y-3">
        <div>
          <MonoText size="sm" muted>Status</MonoText>
          <div className="mt-1 flex items-center gap-2">
            <span className={`inline-block w-2 h-2 rounded-full ${agent.status === 'running' ? 'bg-accent animate-status-blink' : 'bg-text-muted'}`} />
            <span className="text-sm text-text-primary">{agent.status}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Progress</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm text-text-primary">Step {agent.step} / {agent.maxSteps}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Tools Called</MonoText>
          <div className="mt-1"><span className="font-mono text-sm text-text-primary">{agent.toolCalls.length}</span></div>
        </div>
        <div>
          <MonoText size="sm" muted>Evidence Collected</MonoText>
          <div className="mt-1"><span className="font-mono text-sm text-text-primary">{agent.evidence.length}</span></div>
        </div>
        <div>
          <MonoText size="sm" muted>Claims Submitted</MonoText>
          <div className="mt-1"><span className="font-mono text-sm text-text-primary">{agent.claims.length}</span></div>
        </div>
        {agent.thought && (
          <div>
            <MonoText size="sm" muted>Thought</MonoText>
            <p className="text-xs text-text-secondary mt-1 leading-relaxed">{agent.thought}</p>
          </div>
        )}
      </div>
    );
  };

  const renderEventDetail = () => {
    const event = events.find((e) => e.id === selected.id);
    if (!event) return <MonoText muted>Event not found</MonoText>;
    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Type</MonoText>
          <div className="mt-1"><span className="text-sm text-text-primary">{event.type}</span></div>
        </div>
        <div>
          <MonoText size="sm" muted>Timestamp</MonoText>
          <div className="mt-1"><span className="font-mono text-xs text-text-muted">{event.timestamp}</span></div>
        </div>
        {event.agentId && (
          <div>
            <MonoText size="sm" muted>Agent</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={event.agentId} size={18} />
              <span className="text-sm text-text-secondary">{event.agentId}</span>
            </div>
          </div>
        )}
        <div>
          <MonoText size="sm" muted>Message</MonoText>
          <p className="text-sm text-text-secondary mt-1">{event.message}</p>
        </div>
        {event.data && (
          <div>
            <MonoText size="sm" muted>Data</MonoText>
            <pre className="text-xs text-text-muted mt-1 p-2 rounded bg-raised overflow-x-auto font-mono">
              {JSON.stringify(event.data, null, 2)}
            </pre>
          </div>
        )}
      </div>
    );
  };

  const renderContent = () => {
    switch (selected.type) {
      case 'evidence': return renderEvidenceDetail();
      case 'vote': return renderVoteDetail();
      case 'agent': return renderAgentDetail();
      case 'event': return renderEventDetail();
      default: return <MonoText muted>Unknown selection type</MonoText>;
    }
  };

  const titleMap: Record<string, string> = { evidence: 'Evidence', vote: 'Vote', agent: 'Agent', event: 'Event' };

  return (
    <aside className="w-80 shrink-0 border-l border-border-dim bg-base overflow-y-auto">
      <div className="sticky top-0 bg-base border-b border-border-dim px-4 py-3">
        <div className="flex items-center justify-between">
          <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">
            {titleMap[selected.type]}
          </h3>
          <button onClick={() => useUiStore.getState().clearSelection()} className="text-text-muted hover:text-text-primary font-mono text-[10px] cursor-pointer">
            ✕
          </button>
        </div>
        <div className="mt-1"><MonoText size="sm" muted>{selected.id}</MonoText></div>
      </div>
      <div className="p-4 animate-fade-in">{renderContent()}</div>
    </aside>
  );
}
