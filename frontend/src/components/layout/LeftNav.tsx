import { useEffect } from 'react';
import { NavLink } from 'react-router-dom';
import { Pin, Play, CheckCircle, Archive, FileText, FlaskConical, Database, History } from 'lucide-react';
import PaginatedSection from './PaginatedSection';
import { useCaseStore } from '@/stores';
import { useT } from '@/i18n';
import { ACTIVE_CASE_STATUSES, type CaseSummary } from '@/types/case';

interface LeftNavProps {
  cases?: CaseSummary[];
}

const SECTIONS: { title: string; icon: typeof Pin; filter: (c: CaseSummary) => boolean }[] = [
  { title: 'app.pinned', icon: Pin, filter: (c) => c.pinned && !c.archived },
  { title: 'app.running', icon: Play, filter: (c) => !c.archived && ACTIVE_CASE_STATUSES.includes(c.status) },
  { title: 'app.completed', icon: CheckCircle, filter: (c) => !c.archived && c.status === 'RESOLVED' },
  { title: 'app.archived', icon: Archive, filter: (c) => c.archived },
];

const FOOTER_ITEMS = [
  { to: '/templates', icon: FileText, label: 'Templates' },
  { to: '/benchmark', icon: FlaskConical, label: 'Benchmark' },
  { to: '/dataset', icon: Database, label: 'Dataset' },
  { to: '/history', icon: History, label: 'History' },
];

export default function LeftNav({ cases = [] }: LeftNavProps) {
  const t = useT();
  const hasMore = useCaseStore((s) => s.hasMore);
  const loadMore = useCaseStore((s) => s.loadMoreCases);

  useEffect(() => {
    useCaseStore.getState().fetchCases();
  }, []);

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border-dim bg-base">
      <div className="border-b border-border-dim px-4 py-3">
        <h2 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">Decision Center</h2>
      </div>

      <div className="flex-1 overflow-y-auto">
        {SECTIONS.map(({ title, icon: Icon, filter }) => {
          const filtered = cases.filter(filter);
          if (filtered.length === 0 && title !== 'app.pinned' && title !== 'app.archived') return null;

          return (
            <PaginatedSection
              key={title}
              title={t(title)}
              icon={Icon}
              items={filtered}
              collapsible={title === 'app.completed'}
              defaultExpanded={title !== 'app.completed'}
            />
          );
        })}
        {hasMore && (
          <button
            type="button"
            onClick={() => void loadMore()}
            className="w-full px-4 py-2 text-xs text-text-muted hover:text-text-secondary hover:bg-raised transition-colors"

          >
            {t('app.loadMore')}
          </button>
        )}
      </div>

      <div className="border-t border-border-dim py-2">
        {FOOTER_ITEMS.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            className="flex items-center gap-2 px-4 py-1.5 text-xs text-text-muted hover:text-text-secondary hover:bg-raised transition-colors"
          >
            <Icon size={12} />
            {label}
          </NavLink>
        ))}
      </div>
    </aside>
  );
}
