import { describe, it, expect, vi, beforeEach } from 'vitest';
import { subscribeCaseStream } from './stream';
import { useEventStore, useAgentStore, useCaseStore } from '@/stores';

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
    useCaseStore.setState({ case: null, cases: [], loading: false, error: null });
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



  it('incrementally adds evidence from EVIDENCE_CREATED', () => {
    useAgentStore.getState().patchAgent('melchior', { status: 'running' });
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'EVIDENCE_CREATED', agent_code: 'melchior', message: 'm', payload: { evidence_id: 'EV-1', reliability: 0.9, tool_name: 'web_search' }, timestamp: 't' }),
    });
    expect(useAgentStore.getState().agents.melchior?.evidence).toHaveLength(1);
    expect(useAgentStore.getState().agents.melchior?.evidence[0].id).toBe('EV-1');
    unsub();
  });

  it('incrementally updates tool calls through the lifecycle', () => {
    useAgentStore.getState().patchAgent('melchior', { status: 'running' });
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'TOOL_CALL_REQUESTED', agent_code: 'melchior', message: 'm', payload: { tool_call_id: 'call-1', tool_name: 'web_search', arguments: '{"q":"x"}' }, timestamp: 't' }),
    });
    es.onmessage!({
      data: JSON.stringify({ id: 'e2', type: 'TOOL_CALL_COMPLETED', agent_code: 'melchior', message: 'm', payload: { tool_call_id: 'call-1', tool_name: 'web_search', duration_ms: 42, result: 'found' }, timestamp: 't' }),
    });
    const tc = useAgentStore.getState().agents.melchior?.toolCalls[0];
    expect(tc?.name).toBe('web_search');
    expect(tc?.params).toEqual({ q: 'x' });
    expect(tc?.result).toBe('found');
    expect(useAgentStore.getState().agents.melchior?.toolCalls).toHaveLength(1);
    unsub();
  });

  it('incrementally sets the agent vote from VOTE_SUBMITTED', () => {
    useAgentStore.getState().patchAgent('casper', { status: 'running' });
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'VOTE_SUBMITTED', agent_code: 'casper', message: 'm', payload: { stance: 'reject', confidence: 94, reasoning: 'r' }, timestamp: 't' }),
    });
    expect(useAgentStore.getState().agents.casper?.vote?.stance).toBe('reject');
    expect(useAgentStore.getState().agents.casper?.vote?.confidence).toBe(94);
    unsub();
  });
  it('applies CASE_STATUS_CHANGED to caseStore without pushing to the timeline', () => {
    const unsub = subscribeCaseStream('c1');
    const es = FakeEventSource.last!;
    useCaseStore.getState().loadCaseList([
      { id: 'c1', question: 'Q?', status: 'DRAFT', round: 0, createdAt: 't', pinned: false },
    ]);
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'CASE_STATUS_CHANGED', message: 'status', payload: { status: 'DEBATING', round: 2 }, timestamp: 't' }),
    });
    expect(useCaseStore.getState().cases.find((c) => c.id === 'c1')?.status).toBe('DEBATING');
    expect(useEventStore.getState().events).toHaveLength(0);
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

  it('calls onTerminal when CASE_COMPLETED arrives', () => {
    const onTerminal = vi.fn();
    const unsub = subscribeCaseStream('c1', onTerminal);
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'CASE_COMPLETED', message: 'done', timestamp: 't' }),
    });
    expect(onTerminal).toHaveBeenCalledTimes(1);
    unsub();
  });

  it('calls onTerminal when CASE_FAILED arrives', () => {
    const onTerminal = vi.fn();
    const unsub = subscribeCaseStream('c1', onTerminal);
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'CASE_FAILED', message: 'fail', timestamp: 't' }),
    });
    expect(onTerminal).toHaveBeenCalledTimes(1);
    unsub();
  });

  it('does not call onTerminal for non-terminal events', () => {
    const onTerminal = vi.fn();
    const unsub = subscribeCaseStream('c1', onTerminal);
    const es = FakeEventSource.last!;
    es.onmessage!({
      data: JSON.stringify({ id: 'e1', type: 'VOTE_SUBMITTED', message: 'voted', timestamp: 't' }),
    });
    expect(onTerminal).not.toHaveBeenCalled();
    unsub();
  });
});
