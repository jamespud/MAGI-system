import { useEffect, useState } from 'react';
import { api, type ApiUser, type ApiApiKey, type ApiIssuedKey } from '@/api/client';

function KeyList({ userId }: { userId: number }) {
  const [keys, setKeys] = useState<ApiApiKey[] | null>(null);
  const [issued, setIssued] = useState<ApiIssuedKey | null>(null);
  const [name, setName] = useState('');
  const [error, setError] = useState('');

  const load = async () => {
    setError('');
    try {
      const r = await api.listUserKeys(userId);
      setKeys(r.keys);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load keys failed');
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId]);

  const issue = async () => {
    try {
      const k = await api.issueUserKey(userId, name || undefined);
      setIssued(k);
      setName('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'issue failed');
    }
  };

  const revoke = async (keyId: string) => {
    try {
      await api.revokeKey(keyId);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'revoke failed');
    }
  };

  const rotate = async (keyId: string) => {
    try {
      const k = await api.rotateKey(keyId);
      setIssued(k);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'rotate failed');
    }
  };

  return (
    <div className="mt-3 rounded border border-border-dim bg-background/40 p-3 space-y-2">
      <div className="flex gap-2">
        <input
          className="flex-1 rounded border border-border-dim bg-background px-2 py-1 text-xs"
          placeholder="Key name (e.g. ci, laptop)"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button
          className="rounded bg-accent px-3 py-1 text-xs disabled:opacity-50"
          disabled={!keys}
          onClick={() => void issue()}
        >
          Issue key
        </button>
      </div>
      {issued && (
        <div className="rounded border border-accent/40 bg-accent/10 p-2 text-xs space-y-1">
          <p className="font-semibold">New API key — copy it now, it will only be shown once:</p>
          <p className="font-mono break-all select-all">{issued.plaintext}</p>
        </div>
      )}
      {error && <p className="text-xs text-red-500">{error}</p>}
      <ul className="space-y-1">
        {keys?.map((k) => (
          <li key={k.id} className="flex items-center justify-between gap-2 text-xs">
            <span className="font-mono">
              {k.prefix}
              {k.revoked ? ' (revoked)' : ''}
              {k.name ? ` · ${k.name}` : ''}
            </span>
            <span className="flex gap-1">
              {!k.revoked && (
                <>
                  <button
                    className="rounded border border-border-dim px-2 py-0.5"
                    onClick={() => void rotate(k.id)}
                  >
                    Rotate
                  </button>
                  <button
                    className="rounded border border-border-dim px-2 py-0.5"
                    onClick={() => void revoke(k.id)}
                  >
                    Revoke
                  </button>
                </>
              )}
            </span>
          </li>
        ))}
        {keys && keys.length === 0 && <li className="text-text-muted">No keys.</li>}
      </ul>
    </div>
  );
}

export default function Users() {
  const [users, setUsers] = useState<ApiUser[] | null>(null);
  const [expanded, setExpanded] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [role, setRole] = useState('user');
  const [created, setCreated] = useState<ApiIssuedKey | null>(null);
  const [error, setError] = useState('');

  const load = async () => {
    setError('');
    try {
      const r = await api.listUsers();
      setUsers(r.users);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load users failed');
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const create = async () => {
    if (!name.trim()) return;
    try {
      const r = await api.createUser(name.trim(), role);
      setCreated(r.api_key ?? null);
      setName('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create user failed');
    }
  };

  const remove = async (id: number) => {
    try {
      await api.deleteUser(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Users &amp; API Keys</h1>
      <p className="text-sm text-text-muted">
        Manage harness accounts and DB-backed API keys. The plaintext key is shown only once at
        issuance — store it immediately.
      </p>

      {error && <p className="text-sm text-red-500">{error}</p>}

      <div className="rounded border border-border-dim bg-raised p-4 space-y-2">
        <h2 className="text-sm font-medium">Create user</h2>
        <div className="flex gap-2">
          <input
            className="flex-1 rounded border border-border-dim bg-background px-3 py-2 text-sm"
            placeholder="User name (e.g. ci-runner)"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <select
            className="rounded border border-border-dim bg-background px-3 py-2 text-sm"
            value={role}
            onChange={(e) => setRole(e.target.value)}
          >
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
          <button
            className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
            disabled={!name.trim()}
            onClick={() => void create()}
          >
            Create
          </button>
        </div>
        {created && (
          <div className="rounded border border-accent/40 bg-accent/10 p-2 text-xs space-y-1">
            <p className="font-semibold">Bootstrap API key — copy it now, it will only be shown once:</p>
            <p className="font-mono break-all select-all">{created.plaintext}</p>
          </div>
        )}
      </div>

      <div className="space-y-3">
        {!users && <p className="text-sm text-text-muted">Loading…</p>}
        {users?.map((u) => (
          <div key={u.id} className="rounded border border-border-dim bg-raised p-4">
            <button
              className="flex w-full items-center justify-between text-left"
              onClick={() => setExpanded(expanded === u.id ? null : u.id)}
            >
              <span className="font-medium">
                {u.name} <span className="text-xs text-text-muted">(id {u.id})</span>
              </span>
              <span className="flex items-center gap-3 text-xs text-text-muted">
                <span className="rounded bg-raised px-2 py-0.5">{u.role}</span>
                <span>{u.active_keys} active key(s)</span>
                <button
                  className="rounded border border-border-dim px-2 py-0.5"
                  onClick={(e) => {
                    e.stopPropagation();
                    void remove(u.id);
                  }}
                >
                  Delete
                </button>
              </span>
            </button>
            {expanded === u.id && <KeyList userId={u.id} />}
          </div>
        ))}
      </div>
    </div>
  );
}
