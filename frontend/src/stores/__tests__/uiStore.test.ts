import { describe, it, expect, beforeEach } from 'vitest';
import { useUiStore } from '../uiStore';

describe('uiStore', () => {
  beforeEach(() => {
    useUiStore.setState({ selected: null, expandedAgent: null, sidebarCollapsed: false, timelineCollapsed: false, timelineHeight: 192 });
  });

  it('selects an object', () => {
    useUiStore.getState().select({ type: 'evidence', id: 'EV-001' });
    expect(useUiStore.getState().selected?.id).toBe('EV-001');
  });

  it('clears selection', () => {
    useUiStore.getState().select({ type: 'evidence', id: 'EV-001' });
    useUiStore.getState().clearSelection();
    expect(useUiStore.getState().selected).toBeNull();
  });

  it('toggles sidebar', () => {
    useUiStore.getState().toggleSidebar();
    expect(useUiStore.getState().sidebarCollapsed).toBe(true);
  });

  it('toggles timeline', () => {
    useUiStore.getState().toggleTimeline();
    expect(useUiStore.getState().timelineCollapsed).toBe(true);
  });

  it('sets expanded agent', () => {
    useUiStore.getState().setExpandedAgent('melchior');
    expect(useUiStore.getState().expandedAgent).toBe('melchior');
    useUiStore.getState().setExpandedAgent(null);
    expect(useUiStore.getState().expandedAgent).toBeNull();
  });
});
