import { describe, it, expect, vi, beforeEach } from 'vitest';
import { subscribeCaseStream } from './stream';
import { useEventStore, useAgentStore } from '@/stores';

class FakeEventSource {
  static last: FakeEventSource | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  constructor(public url: string) {
    FakeEventSource.last = this;
  }
}

describe('subscribeCaseStream', () => {
  beforeEach(() => {
    FakeEventSource.last = null;
    vi.stubGlobal('EventSource', FakeEventSource);
    useEventStore.getState().clearEvents();
    useAgentStore.getState().resetAgents();
  });

  it('opens an EventSource on /api/v1/cases/:id/stream', () => {
    subscribeCaseStream('c1');
    expect(FakeEventSource.last?.url).toBe('/api/v1/cases/c1/stream');
  });

  it('pushes mapped events into eventStore', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'VOTE_SUBMITTED', message: 'Votes submitted', timestamp: 't' }),
    });
    expect(useEventStore.getState().events).toHaveLength(1);
    expect(useEventStore.getState().events[0].type).toBe('VOTE_SUBMITTED');
    expect(useEventStore.getState().events[0].message).toBe('Votes submitted');
    unsub();
  });

  it('patches agentStore status for agent-scoped events', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'AGENT_STARTED', agent_code: 'melchior', message: 'm', timestamp: 't' }),
    });
    expect(useAgentStore.getState().agents.melchior?.status).toBe('running');
    unsub();
  });

  it('marks agent completed on CASE_COMPLETED', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'CASE_COMPLETED', agent_code: 'melchior', message: 'done', timestamp: 't' }),
    });
    expect(useAgentStore.getState().agents.melchior?.status).toBe('completed');
    unsub();
  });

  it('close() is called on unsubscribe', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    unsub();
    expect(es.close).toHaveBeenCalled();
  });

  it('ignores malformed frames without throwing', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    expect(() => es.onmessage!({ data: 'not json' })).not.toThrow();
    expect(useEventStore.getState().events).toHaveLength(0);
    unsub();
  });
});
