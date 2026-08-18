import { useMemo, useState } from 'react';
import { api, type ApiEvent } from '@/api/client';
import { useT } from '@/i18n';

type ViewMode = 'timeline' | 'trace';

interface TraceLane {
  key: string;
  label: string;
  events: ApiEvent[];
}

function groupByRun(events: ApiEvent[]): Record<string, ApiEvent[]> {
  const out: Record<string, ApiEvent[]> = {};
  for (const e of events) {
    const k = e.run_id || '';
    (out[k] = out[k] || []).push(e);
  }
  return out;
}

function buildTraceLanes(events: ApiEvent[]): TraceLane[] {
  const groups = groupByRun(events);
  return Object.entries(groups).map(([runId, runEvents]) => {
    const agents = [...new Set(runEvents.map((e) => e.agent_code).filter(Boolean))];
    const label = agents.length > 0 ? `${runId || 'case'} · ${agents.join(', ')}` : runId || 'case-level';
    return { key: runId || 'case', label, events: runEvents };
  });
}

function eventCategory(type: string): 'lifecycle' | 'agent' | 'tool' | 'evidence' | 'vote' | 'error' {
  if (type === 'ERROR') return 'error';
  if (type.startsWith('TOOL_')) return 'tool';
  if (type.startsWith('EVIDENCE_') || type.startsWith('CLAIM_')) return 'evidence';
  if (type.startsWith('VOTE_') || type.startsWith('CONSENSUS_')) return 'vote';
  if (type.startsWith('AGENT_') || type.startsWith('ROUND_') || type.startsWith('DEBATE_') ||
    type.startsWith('REFLECTION')) return 'agent';
  return 'lifecycle';
}

const CATEGORY_COLORS: Record<ReturnType<typeof eventCategory>, string> = {
  lifecycle: 'bg-blue-400',
  agent: 'bg-purple-400',
  tool: 'bg-cyan-400',
  evidence: 'bg-emerald-400',
  vote: 'bg-amber-400',
  error: 'bg-red-500',
};

export default function Replay() {
  const t = useT();
  const [caseId, setCaseId] = useState('');
  const [events, setEvents] = useState<ApiEvent[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [mode, setMode] = useState<ViewMode>('timeline');
  const [selectedEvent, setSelectedEvent] = useState<ApiEvent | null>(null);

  const load = async (nextMode: ViewMode = mode) => {
    if (!caseId.trim()) return;
    setBusy(true);
    setError('');
    setSelectedEvent(null);
    try {
      const list = nextMode === 'trace'
        ? await api.getTrace(caseId.trim())
        : await api.getEvents(caseId.trim());
      setEvents(list);
      setMode(nextMode);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
      setEvents([]);
    } finally {
      setBusy(false);
    }
  };

  const lanes = useMemo(() => events ? buildTraceLanes(events) : [], [events]);
  const span = useMemo(() => {
    if (!events || events.length === 0) return { start: 0, duration: 0 };
    const times = events.map((event) => new Date(event.timestamp).getTime()).filter(Number.isFinite);
    const start = Math.min(...times);
    const end = Math.max(...times);
    return { start, duration: Math.max(end - start, 1) };
  }, [events]);
  const traceStats = useMemo(() => ({
    events: events?.length ?? 0,
    runs: lanes.length,
    agents: new Set((events ?? []).map((e) => e.agent_code).filter(Boolean)).size,
    errors: (events ?? []).filter((e) => e.type === 'ERROR').length,
  }), [events, lanes.length]);

  return (
    <div className="p-6 space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">{t('nav.replay')}</h1>
        <div className="rounded border border-border-dim bg-raised p-0.5 flex">
          {(['timeline', 'trace'] as ViewMode[]).map((value) => (
            <button
              key={value}
              onClick={() => void load(value)}
              disabled={!caseId.trim() || busy}
              className={`px-3 py-1 rounded font-mono text-xs transition-colors ${
                mode === value ? 'bg-background text-text-primary' : 'text-text-muted hover:text-text-secondary'
              }`}
            >
              {value === 'timeline' ? t('replay.timeline') : t('replay.trace')}
            </button>
          ))}
        </div>
      </div>

      <div className="flex gap-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder={t('replay.casePlaceholder')}
          value={caseId}
          onChange={(e) => setCaseId(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void load(); }}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={!caseId.trim() || busy}
          onClick={() => void load()}
        >
          {busy ? t('replay.loading') : t('replay.load')}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {events && mode === 'trace' && (
        <section className="space-y-4" aria-label={t('replay.trace')}>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {[
              { label: t('replay.events'), value: traceStats.events },
              { label: t('replay.runs'), value: traceStats.runs },
              { label: t('replay.agents'), value: traceStats.agents },
              { label: t('replay.errors'), value: traceStats.errors },
            ].map(({ label, value }) => (
              <div key={label} className="rounded border border-border-dim bg-raised p-3">
                <p className="font-mono text-[10px] uppercase tracking-wider text-text-muted">{label}</p>
                <p className="mt-1 text-lg">{value}</p>
              </div>
            ))}
          </div>

          <div className="rounded border border-border-dim bg-raised p-4 space-y-4 overflow-x-auto">
            {lanes.map((lane) => (
              <div key={lane.key} className="min-w-[720px] space-y-1">
                <p className="font-mono text-xs text-text-muted">{lane.label} · {lane.events.length}</p>
                <div className="relative h-8 rounded bg-background border border-border-dim">
                  {lane.events.map((event) => {
                    const time = new Date(event.timestamp).getTime();
                    const offset = Number.isFinite(time) ? ((time - span.start) / span.duration) * 100 : 0;
                    const category = eventCategory(event.type);
                    return (
                      <button
                        key={event.id}
                        title={`${event.timestamp} · ${event.type} · ${event.message}`}
                        aria-label={`${event.type}: ${event.message}`}
                        onClick={() => setSelectedEvent(event)}
                        className={`absolute top-1 h-6 w-2 rounded-sm ${CATEGORY_COLORS[category]} ${
                          selectedEvent?.id === event.id ? 'ring-2 ring-white/70' : 'opacity-80 hover:opacity-100'
                        }`}
                        style={{ left: `${Math.min(Math.max(offset, 0), 99)}%` }}
                      />
                    );
                  })}
                </div>
              </div>
            ))}
            {lanes.length === 0 && <p className="text-sm text-text-muted">{t('replay.empty')}</p>}
          </div>

          {selectedEvent && (
            <div className="rounded border border-border-dim bg-raised p-4">
              <div className="flex items-center justify-between gap-3">
                <p className="font-mono text-xs text-text-primary">{selectedEvent.type}</p>
                <p className="font-mono text-xs text-text-muted">{selectedEvent.timestamp}</p>
              </div>
              <p className="mt-2 text-sm text-text-secondary">{selectedEvent.message}</p>
              {selectedEvent.payload && Object.keys(selectedEvent.payload).length > 0 && (
                <pre className="mt-3 overflow-x-auto rounded bg-background p-3 font-mono text-[10px] text-text-muted">
                  {JSON.stringify(selectedEvent.payload, null, 2)}
                </pre>
              )}
            </div>
          )}
        </section>
      )}

      {events && mode === 'timeline' && (
        <ol className="space-y-2">
          {events.length === 0 && <li className="text-sm text-text-muted">{t('replay.empty')}</li>}
          {Object.entries(groupByRun(events)).map(([runId, evs]) => (
            <li key={runId || 'case'} className="rounded border border-border-dim bg-raised p-3">
              <p className="font-mono text-xs text-text-muted mb-2">{runId || 'case-level'} · {evs.length} steps</p>
              <ol className="space-y-1">
                {evs.map((e) => (
                  <li key={e.id} className="rounded bg-background px-3 py-2 text-sm">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs text-text-muted">{e.type}</span>
                      <span className="text-xs text-text-muted">{new Date(e.timestamp).toLocaleTimeString()}</span>
                    </div>
                    <p className="mt-1">{e.message}</p>
                    {e.payload && Object.keys(e.payload).length > 0 && (
                      <details className="mt-1">
                        <summary className="cursor-pointer text-xs text-text-muted">payload</summary>
                        <pre className="mt-1 text-[10px] font-mono whitespace-pre-wrap break-all text-text-muted">{JSON.stringify(e.payload, null, 2)}</pre>
                      </details>
                    )}
                  </li>
                ))}
              </ol>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
