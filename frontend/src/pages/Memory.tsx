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
  const [editingId, setEditingId] = useState('');
  const [question, setQuestion] = useState('');
  const [contextSummary, setContextSummary] = useState('');
  const [resolution, setResolution] = useState('');
  const [learned, setLearned] = useState('');
  const [annotation, setAnnotation] = useState('');
  const [tags, setTags] = useState('');

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

  const startEdit = (memory: ApiMemoryProjection) => {
    setEditingId(memory.CaseID);
    setQuestion(memory.QuestionSummary ?? '');
    setContextSummary(memory.ContextSummary ?? '');
    setResolution(memory.Resolution ?? '');
    setLearned(memory.Outcome?.Learned ?? '');
    setAnnotation(memory.Annotation ?? '');
    setTags((memory.Tags ?? []).join(', '));
  };

  const saveEdit = async (memory: ApiMemoryProjection) => {
    setBusy(true);
    setError('');
    try {
      const updated = await api.updateMemory(memory.CaseID, {
        question_summary: question,
        context_summary: contextSummary,
        resolution,
        learned,
        annotation,
        tags: tags.split(',').map((tag) => tag.trim()).filter(Boolean),
      });
      setResults((current) => current?.map((item) => item.CaseID === updated.CaseID ? updated : item) ?? null);
      setEditingId('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed');
    } finally {
      setBusy(false);
    }
  };

  const remove = async (memory: ApiMemoryProjection) => {
    if (!window.confirm(t('memory.deleteConfirm'))) return;
    setBusy(true);
    setError('');
    try {
      await api.deleteMemory(memory.CaseID);
      setResults((current) => current?.filter((item) => item.CaseID !== memory.CaseID) ?? null);
      if (editingId === memory.CaseID) setEditingId('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
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
              <div className="flex flex-wrap items-start justify-between gap-3">
                <Link to={'/case/' + m.CaseID} className="font-medium hover:underline">
                  {m.QuestionSummary || m.CaseID}
                </Link>
                <div className="flex gap-2">
                  <button
                    className="rounded border border-border-dim px-2 py-1 text-xs hover:bg-background disabled:opacity-50"
                    disabled={busy}
                    onClick={() => editingId === m.CaseID ? setEditingId('') : startEdit(m)}
                  >
                    {editingId === m.CaseID ? t('memory.cancel') : t('memory.edit')}
                  </button>
                  <button
                    className="rounded border border-red-500/50 px-2 py-1 text-xs text-red-500 hover:bg-red-500/10 disabled:opacity-50"
                    disabled={busy}
                    onClick={() => void remove(m)}
                  >
                    {t('memory.delete')}
                  </button>
                </div>
              </div>
              <p className="text-sm text-text-muted">{m.ContextSummary || t('memory.noSummary')}</p>
              <p className="text-sm mt-1">
                Resolution: <span className="font-medium">{m.Resolution || '-'}</span>
                {m.Outcome?.Learned ? ` · learned: ${m.Outcome.Learned}` : ''}
              </p>
              {m.Annotation && <p className="text-sm mt-1">Annotation: {m.Annotation}</p>}
              {!!m.Tags?.length && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {m.Tags.map((tag) => (
                    <span key={tag} className="rounded-full border border-border-dim px-2 py-0.5 text-xs">{tag}</span>
                  ))}
                </div>
              )}
              {editingId === m.CaseID && (
                <div className="mt-3 space-y-2 rounded border border-border-dim bg-background p-3">
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.question')}</span>
                    <input
                      className="w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={question}
                      onChange={(e) => setQuestion(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.context')}</span>
                    <textarea
                      className="min-h-20 w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={contextSummary}
                      onChange={(e) => setContextSummary(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.resolutionLabel')}</span>
                    <textarea
                      className="min-h-20 w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={resolution}
                      onChange={(e) => setResolution(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.learnedLabel')}</span>
                    <textarea
                      className="min-h-20 w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={learned}
                      onChange={(e) => setLearned(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.annotation')}</span>
                    <textarea
                      className="min-h-20 w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={annotation}
                      onChange={(e) => setAnnotation(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-1 text-xs">
                    <span>{t('memory.tags')}</span>
                    <input
                      className="w-full rounded border border-border-dim bg-raised px-2 py-1 text-sm"
                      value={tags}
                      onChange={(e) => setTags(e.target.value)}
                    />
                  </label>
                  <button
                    className="rounded bg-accent px-3 py-1.5 text-sm disabled:opacity-50"
                    disabled={busy}
                    onClick={() => void saveEdit(m)}
                  >
                    {t('memory.save')}
                  </button>
                </div>
              )}
              <p className="text-xs text-text-muted mt-1">{m.CaseID} · v{m.ProjectionVersion}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
