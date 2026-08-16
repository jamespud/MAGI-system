import { useEffect, useState } from 'react';
import { api, type ApiKnowledgeDoc } from '@/api/client';

export default function Knowledge() {
  const [docs, setDocs] = useState<ApiKnowledgeDoc[] | null>(null);
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    setBusy(true);
    setError('');
    try {
      const r = await api.listKnowledge();
      setDocs(r.documents);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const create = async () => {
    if (!title.trim() || !content.trim()) {
      setError('Title and content are required.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const doc = await api.createKnowledge(title.trim(), content.trim());
      setTitle('');
      setContent('');
      await load();
      if (doc.status === 'failed') {
        setError(`Indexed with errors: ${doc.error || 'unknown'}`);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed');
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    setBusy(true);
    setError('');
    try {
      await api.deleteKnowledge(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Knowledge Base</h1>
      <p className="text-sm text-text-muted">
        Upload documents that agents and the Memory search retrieve semantically via RAG.
      </p>

      {error && <p className="text-sm text-red-500">{error}</p>}

      <div className="rounded border border-border-dim bg-raised p-4 space-y-2">
        <h2 className="text-sm font-medium">New document</h2>
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Title (e.g. Postgres tuning guide)"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
        <textarea
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm min-h-[120px]"
          placeholder="Paste the document content here…"
          value={content}
          onChange={(e) => setContent(e.target.value)}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={busy || !title.trim() || !content.trim()}
          onClick={() => void create()}
        >
          {busy ? 'Uploading…' : 'Upload & index'}
        </button>
      </div>

      <div className="space-y-3">
        {busy && docs === null && <p className="text-sm text-text-muted">Loading…</p>}
        {docs && docs.length === 0 && <p className="text-sm text-text-muted">No documents yet.</p>}
        {docs?.map((d) => (
          <div key={d.id} className="rounded border border-border-dim bg-raised p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <p className="font-medium">{d.title}</p>
                <p className="text-xs text-text-muted mt-1">
                  {d.status} · {d.chunks} chunks · {new Date(d.created_at).toLocaleString()}
                </p>
                {d.status === 'failed' && d.error && (
                  <p className="text-xs text-red-500 mt-1">{d.error}</p>
                )}
              </div>
              <button
                className="rounded border border-border-dim px-3 py-1 text-xs disabled:opacity-50"
                disabled={busy}
                onClick={() => void remove(d.id)}
              >
                Delete
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
