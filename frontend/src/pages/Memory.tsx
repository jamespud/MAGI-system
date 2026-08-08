import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, type ApiMemoryProjection } from '@/api/client';

export default function Memory() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ApiMemoryProjection[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const search = async () => {
    if (!query.trim()) return;
    setBusy(true);
    setError('');
    try {
      const r = await api.searchMemory(query.trim());
      setResults(r.results);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'search failed');
      setResults([]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Memory</h1>
      <div className="flex gap-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Search historical decisions (e.g. stack, database, Rust)"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void search(); }}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={!query.trim() || busy}
          onClick={() => void search()}
        >
          {busy ? 'Searching…' : 'Search'}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {results && (
        <div className="space-y-3">
          {results.length === 0 && <p className="text-sm text-text-muted">No memories found.</p>}
          {results.map((m) => (
            <div key={m.CaseID} className="rounded border border-border-dim bg-raised p-4">
              <Link to={`/case/${m.CaseID}`} className="font-medium hover:underline">
                {m.QuestionSummary || m.CaseID}
              </Link>
              <p className="text-sm text-text-muted">{m.ContextSummary || 'no context summary'}</p>
              <p className="text-sm mt-1">
                Resolution: <span className="font-medium">{m.Resolution || '—'}</span>
                {m.Outcome?.Learned ? ` · learned: ${m.Outcome.Learned}` : ''}
              </p>
              <p className="text-xs text-text-muted mt-1">{m.CaseID} · v{m.ProjectionVersion}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
