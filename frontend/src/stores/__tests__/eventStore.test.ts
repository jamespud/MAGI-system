import { describe, it, expect, beforeEach } from 'vitest';
import { useEventStore } from '../eventStore';
import type { MagiEvent } from '@/types/event';

const ev: MagiEvent = { id: 'e1', type: 'TOOL_CALL', timestamp: '', message: 'Test event' };

describe('eventStore', () => {
  beforeEach(() => {
    useEventStore.setState({ events: [], filters: { tool: true, agent: true, evidence: true, vote: true } });
  });

  it('pushes events', () => {
    useEventStore.getState().pushEvent(ev);
    expect(useEventStore.getState().events).toHaveLength(1);
  });

  it('toggles filter', () => {
    useEventStore.getState().toggleFilter('tool');
    expect(useEventStore.getState().filters.tool).toBe(false);
    useEventStore.getState().toggleFilter('tool');
    expect(useEventStore.getState().filters.tool).toBe(true);
  });

  it('clears events', () => {
    useEventStore.getState().pushEvent(ev);
    useEventStore.getState().clearEvents();
    expect(useEventStore.getState().events).toHaveLength(0);
  });

  it('loads events at once', () => {
    useEventStore.getState().loadEvents([ev, { ...ev, id: 'e2' }]);
    expect(useEventStore.getState().events).toHaveLength(2);
  });

  it('dedupes events by id (history replay + live overlap)', () => {
    const { pushEvent } = useEventStore.getState();
    pushEvent({ id: 'e1', type: 'VOTE_SUBMITTED', timestamp: 't', message: 'm' });
    pushEvent({ id: 'e1', type: 'VOTE_SUBMITTED', timestamp: 't', message: 'm' });
    pushEvent({ id: 'e2', type: 'AGENT_STEP', timestamp: 't', message: 'm' });
    expect(useEventStore.getState().events).toHaveLength(2);
  });
});
