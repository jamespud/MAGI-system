import { useState } from 'react';
import { api, type ApiEvent } from '@/api/client';

function groupByRun(events: ApiEvent[]): Record<string, ApiEvent[]> {
  const out: Record<string, ApiEvent[]> = {};
  for (const e of events) {
    const k = e.run_id || '';
    (out[k] = out[k] || []).push(e);
  }
  return out;
}

export default function Replay() {
  const [caseId, setCaseId] = useState('');
  const [events, setEvents] = useState<ApiEvent[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    if (!caseId.trim()) return;
    setBusy(true);
    setError('');
    try {
      const list = await api.getEvents(caseId.trim());
      setEvents(list);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
      setEvents([]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Replay</h1>
      <div className="flex gap-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Case ID (e.g. case-…) or select from History"
          value={caseId}
          onChange={(e) => setCaseId(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void load(); }}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={!caseId.trim() || busy}
          onClick={() => void load()}
        >
          {busy ? 'Loading…' : 'Replay'}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {events && (
        <ol className="space-y-2">
          {events.length === 0 && <li className="text-sm text-text-muted">No events for this case.</li>}
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
