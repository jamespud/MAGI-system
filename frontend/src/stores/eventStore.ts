import { create } from 'zustand';
import type { MagiEvent, EventFilter } from '@/types/event';

const defaultFilters: EventFilter = { tool: true, agent: true, evidence: true, vote: true };

interface EventState {
  events: MagiEvent[];
  filters: EventFilter;
  pushEvent: (event: MagiEvent) => void;
  loadEvents: (events: MagiEvent[]) => void;
  clearEvents: () => void;
  toggleFilter: (key: keyof EventFilter) => void;
}

export const useEventStore = create<EventState>((set) => ({
  events: [],
  filters: { ...defaultFilters },

  pushEvent: (event) =>
    set((s) => (s.events.some((e) => e.id === event.id) ? s : { events: [...s.events, event] })),

  loadEvents: (events) => set({ events }),

  clearEvents: () => set({ events: [] }),

  toggleFilter: (key) =>
    set((s) => ({
      filters: { ...s.filters, [key]: !s.filters[key] },
    })),
}));
