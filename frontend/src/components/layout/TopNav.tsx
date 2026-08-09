import { NavLink } from 'react-router-dom';
import { Activity, Database, Play, BarChart3, Wrench, Settings, Layers, ShieldCheck } from 'lucide-react';
import { PulseDot, MonoText } from '@/components/shared';

const NAV_ITEMS = [
  { to: '/', icon: Activity, label: 'Decision' },
  { to: '/memory', icon: Layers, label: 'Memory' },
  { to: '/replay', icon: Play, label: 'Replay' },
  { to: '/approvals', icon: ShieldCheck, label: 'Approvals' },
  { to: '/evaluation', icon: BarChart3, label: 'Evaluation' },
  { to: '/dataset', icon: Database, label: 'Dataset' },
  { to: '/tools', icon: Wrench, label: 'Tools' },
  { to: '/settings', icon: Settings, label: 'Settings' },
];

export default function TopNav() {
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
        <MonoText size="sm">Claude Opus 4</MonoText>
        <MonoText size="sm" muted>$0.14</MonoText>
        <MonoText size="sm" muted>1450 Tokens</MonoText>
        <div className="flex items-center gap-1.5">
          <PulseDot color="var(--accent)" size={6} />
          <MonoText size="sm" muted>Running</MonoText>
        </div>
        <div className="flex items-center gap-1.5 text-text-muted">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-success" />
          <MonoText size="sm" muted>Connected</MonoText>
        </div>
      </div>
    </header>
  );
}
