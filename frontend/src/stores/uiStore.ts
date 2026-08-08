import { create } from 'zustand';
import type { AgentId } from '@/types/agent';

export type SelectionType = 'tool_call' | 'evidence' | 'claim' | 'vote' | 'event';

interface Selection {
  type: SelectionType;
  id: string;
  data?: Record<string, unknown>;
}

interface UIState {
  selected: Selection | null;
  expandedAgent: AgentId | null;
  sidebarCollapsed: boolean;
  timelineCollapsed: boolean;
  timelineHeight: number;
  select: (sel: Selection) => void;
  clearSelection: () => void;
  setExpandedAgent: (id: AgentId | null) => void;
  toggleSidebar: () => void;
  toggleTimeline: () => void;
  setTimelineHeight: (h: number) => void;
}

export const useUiStore = create<UIState>((set) => ({
  selected: null,
  expandedAgent: null,
  sidebarCollapsed: false,
  timelineCollapsed: false,
  timelineHeight: 192,

  select: (sel) => set({ selected: sel }),
  clearSelection: () => set({ selected: null }),
  setExpandedAgent: (id) => set({ expandedAgent: id }),
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
  toggleTimeline: () => set((s) => ({ timelineCollapsed: !s.timelineCollapsed })),
  setTimelineHeight: (h) => set({ timelineHeight: h }),
}));
