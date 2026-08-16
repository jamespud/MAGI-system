import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, type ApiMemoryProjection } from '@/api/client';
import { useT } from '@/i18n';

export default function Memory() {
  const t = useT();
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
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('memory.title')}</h1>
        <button
          className="rounded border border-accent/50 px-3 py-1.5 text-sm hover:bg-[var(--accent)]/10 transition-colors"
          onClick={() => void api.exportMemory().catch(() => {})}
        >
          Export (JSON)
        </button>
      </div>
      <div className="flex gap-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder={t('memory.searchPlaceholder')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void search(); }}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={!query.trim() || busy}
          onClick={() => void search()}
        >
          {busy ? t('memory.searching') : t('memory.search')}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {results && (
        <div className="space-y-3">
          {results.length === 0 && <p className="text-sm text-text-muted">{t('memory.noMemories')}</p>}
          {results.map((m) => (
            <div key={m.CaseID} className="rounded border border-border-dim bg-raised p-4">
              <Link to={`/case/${m.CaseID}`} className="font-medium hover:underline">
                {m.QuestionSummary || m.CaseID}
              </Link>
              <p className="text-sm text-text-muted">{m.ContextSummary || t('memory.noSummary')}</p>
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
