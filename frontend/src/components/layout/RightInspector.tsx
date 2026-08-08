import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useUiStore, useAgentStore, useEventStore } from '@/stores';
import { MonoText, AgentAvatar } from '@/components/shared';
import { Globe, Link2, Shield, Clock, FileText } from 'lucide-react';
import { api } from '@/api/client';
import type { ApiEvidence } from '@/api/client';
import type { AgentId, AgentSnapshot } from '@/types/agent';
import { stanceColor, stanceLabel } from '@/lib/stance';

export default function RightInspector() {
  const selected = useUiStore((s) => s.selected);
  const agents = useAgentStore((s) => s.agents);
  const events = useEventStore((s) => s.events);
  const { caseId } = useParams<{ caseId: string }>();
  const [evidenceMap, setEvidenceMap] = useState<Record<string, ApiEvidence>>({});

  useEffect(() => {
    if (!caseId) return;
    api.getEvidence(caseId)
      .then((evs) => {
        const m: Record<string, ApiEvidence> = {};
        for (const e of evs) m[e.id] = e;
        setEvidenceMap(m);
      })
      .catch(() => setEvidenceMap({}));
  }, [caseId]);

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
    const agentId = selected.data?.agentId as AgentId | undefined;
    const ref = agentId
      ? agents[agentId]?.evidence.find((e) => e.id === selected.id)
      : Object.values(agents).flatMap((a) => (a ? a.evidence : [])).find((e) => e.id === selected.id);
    const full = evidenceMap[selected.id];
    const ev = full ?? ref;
    if (!ev) return <MonoText muted>Evidence {selected.id} not found</MonoText>;
    const source = full?.source ?? ref?.source ?? '';
    const url = full?.url ?? ref?.url;
    const observation = full?.observation ?? ref?.observation ?? '';
    const reliability = full?.reliability ?? ref?.reliability ?? 0;
    const collectedBy = full?.collected_by ?? agentId ?? '';
    const timestamp = full?.timestamp ?? ref?.timestamp ?? '';
    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Source</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Globe size={14} className="text-text-muted" />
            <span className="text-sm text-text-primary">{source}</span>
          </div>
        </div>
        {url && (
          <div>
            <MonoText size="sm" muted>URL</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Link2 size={14} className="text-text-muted" />
              <span className="text-xs text-accent truncate block">{url}</span>
            </div>
          </div>
        )}
        <div>
          <MonoText size="sm" muted>Observation</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed whitespace-pre-wrap break-words">{observation}</p>
        </div>
        <div className="flex items-center gap-4">
          <div>
            <MonoText size="sm" muted>Reliability</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Shield size={14} className="text-accent" />
              <span className="font-mono text-sm font-semibold text-accent">{reliability.toFixed(2)}</span>
            </div>
          </div>
          <div>
            <MonoText size="sm" muted>Collected By</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={collectedBy as AgentId} size={18} />
              <span className="text-sm text-text-secondary">{collectedBy}</span>
            </div>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Timestamp</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Clock size={14} className="text-text-muted" />
            <span className="font-mono text-xs text-text-muted">{timestamp}</span>
          </div>
        </div>
      </div>
    );
  };

  const renderVoteDetail = () => {
    const agentVotes = Object.entries(agents)
      .filter(([, a]) => a?.vote?.stance)
      .map(([id, a]) => ({ agentId: id, ...a!.vote! }));

    const voteAgentId = (selected.data?.agentId as AgentId | undefined) ?? selected.id.replace(/^vote-/, '');
    const vote = agentVotes.find((v) => v.agentId === voteAgentId) || agentVotes[0];
    if (!vote) return <MonoText muted>No vote data available</MonoText>;

    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Stance</MonoText>
          <div className="mt-1">
            <span className="font-mono text-base font-bold" style={{ color: stanceColor(vote.stance) }}>
              {stanceLabel(vote.stance)}
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

  const renderToolCallDetail = () => {
    const agentId = selected.data?.agentId as AgentId | undefined;
    const entries: [AgentId, AgentSnapshot | null][] = agentId
      ? [[agentId, agents[agentId] ?? null]]
      : (Object.keys(agents) as AgentId[]).map((k): [AgentId, AgentSnapshot | null] => [k, agents[k]]);
    for (const [id, a] of entries) {
      if (!a) continue;
      const tc = a.toolCalls.find((t) => t.id === selected.id);
      if (!tc) continue;
      return (
        <div className="space-y-4">
          <div>
            <MonoText size="sm" muted>Agent</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={id} size={18} />
              <span className="text-sm text-text-secondary">{id}</span>
            </div>
          </div>
          <div>
            <MonoText size="sm" muted>Tool</MonoText>
            <div className="mt-1"><span className="font-mono text-sm text-accent">{tc.name}</span></div>
          </div>
          <div>
            <MonoText size="sm" muted>Arguments</MonoText>
            <pre className="text-xs text-text-secondary mt-1 whitespace-pre-wrap break-words font-mono bg-raised rounded p-2">{JSON.stringify(tc.params, null, 2)}</pre>
          </div>
          {tc.result && (
            <div>
              <MonoText size="sm" muted>Result</MonoText>
              <pre className="text-xs text-text-secondary mt-1 whitespace-pre-wrap break-words font-mono bg-raised rounded p-2 max-h-72 overflow-y-auto">{tc.result}</pre>
            </div>
          )}
          {tc.error && (
            <div>
              <MonoText size="sm" muted>Error</MonoText>
              <pre className="text-xs text-error mt-1 whitespace-pre-wrap break-words font-mono bg-raised rounded p-2">{tc.error}</pre>
            </div>
          )}
          <div className="flex items-center gap-4">
            {tc.durationMs != null && (
              <div>
                <MonoText size="sm" muted>Duration</MonoText>
                <div className="mt-1"><span className="font-mono text-sm text-text-primary">{tc.durationMs}ms</span></div>
              </div>
            )}
            <div>
              <MonoText size="sm" muted>Tool Call ID</MonoText>
              <div className="mt-1"><span className="font-mono text-xs text-text-muted">{tc.id}</span></div>
            </div>
          </div>
        </div>
      );
    }
    return <MonoText muted>Tool call not found</MonoText>;
  };

  const renderClaimDetail = () => {
    const agentId = selected.data?.agentId as AgentId | undefined;
    const scoped = agentId
      ? ([[agentId, agents[agentId] ?? null]] as [AgentId, AgentSnapshot | null][])
      : (Object.keys(agents) as AgentId[]).map((k): [AgentId, AgentSnapshot | null] => [k, agents[k]]);
    const claim = scoped
      .flatMap(([id, a]) => a ? a.claims.map((cl) => ({ ...cl, created_by: id })) : [])
      .find((cl) => cl.id === selected.id);
    if (!claim) return <MonoText muted>Claim not found</MonoText>;
    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Created By</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <AgentAvatar agentId={claim.created_by as AgentId} size={18} />
            <span className="text-sm text-text-secondary">{claim.created_by}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Statement</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed">{claim.text}</p>
        </div>
        <div>
          <MonoText size="sm" muted>Supports</MonoText>
          {claim.supports.length === 0 ? (
            <p className="text-xs text-text-muted mt-1">None</p>
          ) : (
            <ul className="mt-1 space-y-1">
              {claim.supports.map((evId) => {
                const ev = evidenceMap[evId] ?? agents[claim.created_by as AgentId]?.evidence.find((e) => e.id === evId);
                return (
                  <li key={evId} className="text-xs text-text-secondary font-mono">
                    {evId}
                    {ev && <span className="text-text-muted"> — {ev.source}</span>}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
        <div>
          <MonoText size="sm" muted>Contradicts</MonoText>
          {claim.contradicts.length === 0 ? (
            <p className="text-xs text-text-muted mt-1">None</p>
          ) : (
            <ul className="mt-1 space-y-1">
              {claim.contradicts.map((cId) => (
                <li key={cId} className="text-xs text-text-secondary font-mono">{cId}</li>
              ))}
            </ul>
          )}
        </div>
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
      case 'tool_call': return renderToolCallDetail();
      case 'evidence': return renderEvidenceDetail();
      case 'claim': return renderClaimDetail();
      case 'vote': return renderVoteDetail();
      case 'event': return renderEventDetail();
      default: return <MonoText muted>Unknown selection type</MonoText>;
    }
  };

  const titleMap: Record<string, string> = { tool_call: 'Tool Call', evidence: 'Evidence', claim: 'Claim', vote: 'Vote', event: 'Event' };

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
