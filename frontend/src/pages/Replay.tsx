import { useState } from 'react';
import { api, type ApiEvent } from '@/api/client';

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
          {events.map((e) => (
            <li key={e.id} className="rounded border border-border-dim bg-raised px-4 py-2 text-sm">
              <div className="flex items-center justify-between">
                <span className="font-mono text-xs text-text-muted">{e.type}</span>
                <span className="text-xs text-text-muted">{new Date(e.timestamp).toLocaleTimeString()}</span>
              </div>
              <p className="mt-1">{e.message}</p>
              {e.run_id && <p className="text-xs text-text-muted mt-1">run: {e.run_id}</p>}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
