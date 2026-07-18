import { useEventStore, useUiStore } from '@/stores';
import { MonoText, AgentAvatar } from '@/components/shared';
import { Wrench, User, FileSearch, Vote, ChevronUp, ChevronDown } from 'lucide-react';
import { format } from 'date-fns';
import type { EventType, EventFilter } from '@/types/event';

const EVENT_ICONS: Record<EventType, React.ReactNode> = {
  TOOL_CALL: <Wrench size={12} />,
  AGENT_STEP: <User size={12} />,
  EVIDENCE_CREATED: <FileSearch size={12} />,
  VOTE_SUBMITTED: <Vote size={12} />,
  CONSENSUS_CHANGED: <Vote size={12} />,
  ROUND_START: <FileSearch size={12} />,
  DEBATE_START: <User size={12} />,
  REFLECTION: <User size={12} />,
  RESOLVED: <Vote size={12} />,
  ERROR: <FileSearch size={12} />,
};

const FILTER_KEYS: { key: keyof EventFilter; label: string }[] = [
  { key: 'tool', label: 'Tool' },
  { key: 'agent', label: 'Agent' },
  { key: 'evidence', label: 'Evidence' },
  { key: 'vote', label: 'Vote' },
];

const EVENT_TO_FILTER: Record<EventType, keyof EventFilter> = {
  TOOL_CALL: 'tool',
  AGENT_STEP: 'agent',
  EVIDENCE_CREATED: 'evidence',
  VOTE_SUBMITTED: 'vote',
  CONSENSUS_CHANGED: 'vote',
  ROUND_START: 'agent',
  DEBATE_START: 'agent',
  REFLECTION: 'agent',
  RESOLVED: 'vote',
  ERROR: 'agent',
};

export default function BottomTimeline() {
  const events = useEventStore((s) => s.events);
  const filters = useEventStore((s) => s.filters);
  const toggleFilter = useEventStore((s) => s.toggleFilter);
  const timelineCollapsed = useUiStore((s) => s.timelineCollapsed);
  const toggleTimeline = useUiStore((s) => s.toggleTimeline);
  const timelineHeight = useUiStore((s) => s.timelineHeight);

  const filteredEvents = events.filter((e) => filters[EVENT_TO_FILTER[e.type]]);

  return (
    <div
      className="shrink-0 border-t border-border-dim bg-base flex flex-col"
      style={{ height: timelineCollapsed ? 32 : timelineHeight }}
    >
      <div className="flex items-center justify-between px-4 h-8 border-b border-border-dim">
        <div className="flex items-center gap-3">
          <button onClick={toggleTimeline} className="text-text-muted hover:text-text-primary cursor-pointer">
            {timelineCollapsed ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          </button>
          <span className="font-mono text-[10px] text-text-muted uppercase tracking-wider">Timeline</span>
          <span className="font-mono text-[10px] text-text-muted">{events.length} events</span>
        </div>

        {!timelineCollapsed && (
          <div className="flex items-center gap-1">
            {FILTER_KEYS.map(({ key, label }) => (
              <button
                key={key}
                onClick={() => toggleFilter(key)}
                className={`px-2 py-0.5 rounded font-mono text-[10px] transition-colors cursor-pointer ${
                  filters[key]
                    ? 'bg-elevated text-text-primary border border-border-dim'
                    : 'text-text-muted hover:text-text-secondary'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
        )}
      </div>

      {!timelineCollapsed && (
        <div className="flex-1 overflow-y-auto font-mono text-xs">
          {filteredEvents.map((event, i) => (
            <div
              key={event.id}
              className="flex items-center gap-2 px-4 py-1 border-b border-border-dim animate-slide-up hover:bg-raised cursor-pointer"
              style={{ animationDelay: `${i * 20}ms` }}
            >
              <MonoText size="sm" muted>
                {format(new Date(event.timestamp), 'HH:mm')}
              </MonoText>
              <span className="text-text-muted">{EVENT_ICONS[event.type]}</span>
              {event.agentId && <AgentAvatar agentId={event.agentId} size={16} />}
              <span className="text-text-secondary truncate">{event.message}</span>
            </div>
          ))}
          {filteredEvents.length === 0 && (
            <div className="px-4 py-2 text-text-muted text-xs italic">
              No events matching current filters
            </div>
          )}
        </div>
      )}
    </div>
  );
}
