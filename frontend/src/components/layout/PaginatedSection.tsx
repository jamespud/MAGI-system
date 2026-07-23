import { useState, useEffect, useRef } from 'react';
import type { ComponentType } from 'react';
import { NavLink } from 'react-router-dom';
import { ChevronLeft, ChevronRight, ChevronUp } from 'lucide-react';
import { MonoText } from '@/components/shared';
import type { CaseSummary } from '@/types/case';

interface PaginatedSectionProps {
  title: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  icon: ComponentType<any>;
  items: CaseSummary[];
  pageSize?: number;
  collapsible?: boolean;
  defaultExpanded?: boolean;
}

export default function PaginatedSection({
  title, icon: Icon, items, pageSize = 10,
  collapsible = false, defaultExpanded = true,
}: PaginatedSectionProps) {
  const maxPage = Math.max(1, Math.ceil(items.length / pageSize));
  const [page, setPage] = useState(1);
  const [expanded, setExpanded] = useState(defaultExpanded);
  const listRef = useRef<HTMLDivElement>(null);

  // Clamp page when the item list shrinks (e.g. a case leaves the section).
  useEffect(() => {
    if (page > maxPage) setPage(maxPage);
  }, [page, maxPage]);

  const start = (page - 1) * pageSize;
  const pageItems = items.slice(start, start + pageSize);
  const isEmpty = items.length === 0;
  const isCollapsed = collapsible && !expanded;

  return (
    <div className="border-b border-border-dim last:border-b-0">
      {collapsible && !isEmpty ? (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex items-center gap-2 px-4 py-2 w-full text-left hover:bg-raised cursor-pointer transition-colors"
        >
          <Icon size={12} className="text-text-muted" />
          <MonoText size="sm" muted>{title}</MonoText>
          {isCollapsed ? (
            <span className="ml-auto font-mono text-xs text-text-muted bg-raised px-1.5 py-0.5 rounded">
              {items.length}
            </span>
          ) : (
            <ChevronUp size={14} className="ml-auto text-text-muted" />
          )}
        </button>
      ) : (
        <div className="flex items-center gap-2 px-4 py-2">
          <Icon size={12} className="text-text-muted" />
          <MonoText size="sm" muted>{title}</MonoText>
        </div>
      )}
      {!isCollapsed && (
        <>
          {isEmpty ? (
            <p className="px-6 py-1.5 text-xs text-text-muted italic">No cases</p>
          ) : (
            <>
              <div ref={listRef} data-testid="page-list" className={maxPage > 1 ? 'h-[340px] overflow-y-auto' : ''}>
                {pageItems.map((c) => (
                  <NavLink
                    key={c.id}
                    to={`/case/${c.id}`}
                    className={({ isActive }) =>
                      `block px-6 py-1.5 text-sm transition-colors truncate ${
                        isActive ? 'bg-accent/10 text-accent border-r-2 border-accent' : 'text-text-secondary hover:bg-raised hover:text-text-primary'
                      }`
                    }
                  >
                    {c.question.length > 28 ? c.question.slice(0, 28) + '...' : c.question}
                  </NavLink>
                ))}
              </div>
              {maxPage > 1 && (
                <div className="flex items-center justify-between px-4 py-1.5 border-t border-border-dim">
                  <button
                    type="button"
                    onClick={() => setPage((p) => Math.max(p - 1, 1))}
                    disabled={page <= 1}
                    aria-label={`Previous ${title} page`}
                    className="text-text-muted hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
                  >
                    <ChevronLeft size={14} />
                  </button>
                  <MonoText size="sm" muted>{page}/{maxPage}</MonoText>
                  <button
                    type="button"
                    onClick={() => setPage((p) => Math.min(p + 1, maxPage))}
                    disabled={page >= maxPage}
                    aria-label={`Next ${title} page`}
                    className="text-text-muted hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
                  >
                    <ChevronRight size={14} />
                  </button>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}
