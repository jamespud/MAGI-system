import { useEffect, useState } from 'react';
import { NavLink } from 'react-router-dom';
import { Activity, BookOpen, Database, Play, BarChart3, Wrench, Settings, Layers, ShieldCheck, Users as UsersIcon } from 'lucide-react';
import { PulseDot, MonoText } from '@/components/shared';
import { api, type ApiStatus } from '@/api/client';
import { useAgentStore } from '@/stores';

const NAV_ITEMS = [
  { to: '/', icon: Activity, label: 'Decision' },
  { to: '/memory', icon: Layers, label: 'Memory' },
  { to: '/replay', icon: Play, label: 'Replay' },
  { to: '/approvals', icon: ShieldCheck, label: 'Approvals' },
  { to: '/evaluation', icon: BarChart3, label: 'Evaluation' },
  { to: '/dataset', icon: Database, label: 'Dataset' },
  { to: '/tools', icon: Wrench, label: 'Tools' },
  { to: '/knowledge', icon: BookOpen, label: 'Knowledge' },
  { to: '/admin/users', icon: UsersIcon, label: 'Users' },
  { to: '/settings', icon: Settings, label: 'Settings' },
];

export default function TopNav() {
  const [status, setStatus] = useState<ApiStatus | null>(null);
  const [offline, setOffline] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const s = await api.getStatus();
        if (!cancelled) { setStatus(s); setOffline(false); useAgentStore.getState().setMaxSteps(s.max_steps); }
      } catch {
        if (!cancelled) setOffline(true);
      }
    };
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  return (
    <header className="flex h-12 items-center justify-between border-b border-border-dim bg-base px-4 shrink-0">
      <div className="flex items-center gap-6">
        <NavLink to="/" className="flex items-center gap-2">
          <span className="font-mono text-lg font-bold text-accent tracking-widest">MAGI</span>
        </NavLink>

        <nav className="flex items-center gap-1">
          {NAV_ITEMS.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                `flex items-center gap-1.5 rounded-md px-3 py-1.5 font-mono text-xs font-medium tracking-wider uppercase transition-colors ${
                  isActive
                    ? 'bg-accent/10 text-accent'
                    : 'text-text-muted hover:text-text-secondary hover:bg-raised'
                }`
              }
            >
              <Icon size={14} />
              {label}
            </NavLink>
          ))}
        </nav>
      </div>

      <div className="flex items-center gap-4 font-mono text-xs text-text-muted">
        <MonoText size="sm">{status?.model_name || '—'}</MonoText>
        <MonoText size="sm" muted>${(status?.cost_usd ?? 0).toFixed(2)}</MonoText>
        <MonoText size="sm" muted>{(status?.tokens_total ?? 0).toLocaleString()} Tokens</MonoText>
        <div className="flex items-center gap-1.5">
          <PulseDot color="var(--accent)" size={6} />
          <MonoText size="sm" muted>{(status?.runs_active ?? 0) > 0 ? 'Running' : 'Idle'}</MonoText>
        </div>
        <div className="flex items-center gap-1.5 text-text-muted">
          <span className={`inline-block w-1.5 h-1.5 rounded-full ${offline ? 'bg-error' : 'bg-success'}`} />
          <MonoText size="sm" muted>{offline ? 'Offline' : 'Connected'}</MonoText>
        </div>
      </div>
    </header>
  );
}
