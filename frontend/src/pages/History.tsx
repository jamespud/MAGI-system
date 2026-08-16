import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, type ApiCaseResponse } from '@/api/client';
import { useT } from '@/i18n';

export default function History() {
  const t = useT();
  const [cases, setCases] = useState<ApiCaseResponse[]>([]);
  const [filter, setFilter] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const list = await api.getCases();
        if (!cancelled) {
          setCases([...list].sort((a, b) => b.created_at.localeCompare(a.created_at)));
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'failed to load history');
      } finally {
        if (!cancelled) setBusy(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return cases;
    return cases.filter((c) =>
      c.question.toLowerCase().includes(q) ||
      c.status.toLowerCase().includes(q) ||
      c.final_decision.toLowerCase().includes(q),
    );
  }, [cases, filter]);

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('history.title')}</h1>
        <input
          className="w-64 rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Filter by question/status/decision"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>
      {error && <p className="text-red-500">{error}</p>}
      {busy && <p className="text-sm text-text-muted">Loading…</p>}
      {!busy && filtered.length === 0 && (
        <p className="text-sm text-text-muted">{cases.length === 0 ? 'No decisions yet.' : 'No matches.'}</p>
      )}
      <div className="space-y-3">
        {filtered.map((c) => (
          <Link key={c.id} to={`/case/${c.id}`} className="block rounded border border-border-dim bg-raised p-4 hover:border-accent">
            <div className="flex items-center justify-between">
              <p className="font-medium">{c.question}</p>
              <span className="rounded bg-background px-2 py-0.5 text-xs text-text-muted">{c.status}</span>
            </div>
            <p className="text-sm text-text-muted mt-1">
              {c.final_decision || 'pending'} · {new Date(c.created_at).toLocaleString()}
            </p>
          </Link>
        ))}
      </div>
    </div>
  );
}
