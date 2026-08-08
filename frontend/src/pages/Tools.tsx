import { useEffect, useState } from 'react';
import { api } from '@/api/client';

interface ToolInfo {
  name: string;
  desc: string;
}

export default function Tools() {
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const list = await api.listTools();
        if (!cancelled) setTools(list);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'load failed');
      } finally {
        if (!cancelled) setBusy(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Tools</h1>
      {error && <p className="text-red-500">{error}</p>}
      {busy && <p className="text-sm text-text-muted">Loading…</p>}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {tools.map((t) => (
          <div key={t.name} className="rounded border border-border-dim bg-raised p-4">
            <p className="font-mono font-medium">{t.name}</p>
            <p className="text-sm text-text-muted mt-1">{t.desc}</p>
          </div>
        ))}
        {!busy && tools.length === 0 && <p className="text-sm text-text-muted">No tools available.</p>}
      </div>
    </div>
  );
}
