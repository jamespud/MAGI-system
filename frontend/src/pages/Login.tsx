import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useAuthStore } from '@/stores';

export default function Login() {
  const navigate = useNavigate();
  const [key, setKey] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const setStoredKey = useAuthStore((s) => s.setApiKey);
  const clearStoredKey = useAuthStore((s) => s.clearApiKey);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const trimmed = key.trim();
      // Store the candidate key so verifyAuth() carries it.
      setStoredKey(trimmed);
      const ok = await api.verifyAuth();
      if (ok) {
        navigate('/', { replace: true });
        return;
      }
      clearStoredKey();
      setError('This API key was rejected. Check the key, or use "open mode" if authentication is disabled.');
    } finally {
      setBusy(false);
    }
  };

  const skipOpen = () => {
    clearStoredKey();
    navigate('/', { replace: true });
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-base p-6">
      <div className="w-full max-w-sm rounded-lg border border-border-dim bg-raised p-6 space-y-4">
        <div>
          <h1 className="text-xl font-semibold">MAGI · Sign in</h1>
          <p className="text-sm text-text-muted mt-1">
            Enter the API key configured in <span className="font-mono">backend/conf/magi.yaml</span> (
            <span className="font-mono">auth.api_keys</span>).
          </p>
        </div>

        <form onSubmit={(e) => void submit(e)} className="space-y-3">
          <input
            type="password"
            autoComplete="current-password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="API key (X-API-Key)"
            className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm font-mono"
            aria-label="API key"
          />
          {error && <p className="text-sm text-red-500">{error}</p>}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded bg-accent px-4 py-2 text-sm font-medium disabled:opacity-50"
          >
            {busy ? 'Verifying…' : 'Sign in'}
          </button>
        </form>

        <div className="flex items-center gap-2 text-xs text-text-muted">
          <span className="flex-1 border-t border-border-dim" />
          or
          <span className="flex-1 border-t border-border-dim" />
        </div>

        <button
          type="button"
          onClick={skipOpen}
          className="w-full rounded border border-border-dim px-4 py-2 text-sm text-text-secondary hover:bg-base"
        >
          Continue without a key (open mode)
        </button>

        <p className="text-xs text-text-muted">
          The key is stored only in your browser (localStorage) and sent as the{' '}
          <span className="font-mono">X-API-Key</span> header. It is never transmitted by any other
          means. <Link to="/" className="underline">Back to workspace</Link>
        </p>
      </div>
    </div>
  );
}
