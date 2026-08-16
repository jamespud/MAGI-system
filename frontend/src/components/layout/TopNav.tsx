import { useEffect, useState } from 'react';
import { NavLink } from 'react-router-dom';
import { Activity, BookOpen, Database, Play, BarChart3, Wrench, Settings, Layers, ShieldCheck, Users as UsersIcon } from 'lucide-react';
import { PulseDot, MonoText } from '@/components/shared';
import { api, type ApiStatus } from '@/api/client';
import { useAgentStore } from '@/stores';
import { useT, useLang } from '@/i18n';

const NAV_ITEMS = [
  { to: '/', icon: Activity, label: 'nav.decision' },
  { to: '/memory', icon: Layers, label: 'nav.memory' },
  { to: '/replay', icon: Play, label: 'nav.replay' },
  { to: '/approvals', icon: ShieldCheck, label: 'nav.approvals' },
  { to: '/evaluation', icon: BarChart3, label: 'nav.evaluation' },
  { to: '/dataset', icon: Database, label: 'nav.dataset' },
  { to: '/tools', icon: Wrench, label: 'nav.tools' },
  { to: '/knowledge', icon: BookOpen, label: 'nav.knowledge' },
  { to: '/admin/users', icon: UsersIcon, label: 'nav.users' },
  { to: '/settings', icon: Settings, label: 'nav.settings' },
];

export default function TopNav() {
  const t = useT();
  const { lang, setLang } = useLang();
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
              {t(label)}
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
        <button
          type="button"
          onClick={() => setLang(lang === 'en' ? 'zh' : 'en')}
          className="rounded border border-border-dim px-2 py-0.5 text-[10px] text-text-muted hover:text-text-primary hover:bg-raised transition-colors cursor-pointer"
          title="Switch language"
        >
          {lang === 'en' ? '中文' : 'EN'}
        </button>
      </div>
    </header>
  );
}
