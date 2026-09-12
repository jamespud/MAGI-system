import { describe, it, expect, vi, beforeEach } from 'vitest';
import { subscribeCaseStream } from './stream';
import { useEventStore, useAgentStore, useCaseStore } from '@/stores';
import { UNAUTHORIZED_EVENT, clearApiKey } from './client';

const encoder = new TextEncoder();

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

// setupSSE stubs fetch with a controllable ReadableStream of SSE frames and
// returns helpers to emit/close frames and inspect the fetch call.
function setupSSE() {
  let controller: ReadableStreamDefaultController<Uint8Array> | null = null;
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, body: stream } as Response);
  vi.stubGlobal('fetch', fetchMock);
  return {
    fetchMock,
    emit(obj: unknown) {
      controller?.enqueue(encoder.encode(`data: ${JSON.stringify(obj)}\n\n`));
    },
    emitRaw(text: string) {
      controller?.enqueue(encoder.encode(text));
    },
    close() {
      controller?.close();
    },
  };
}

async function subscribeReady(caseId = 'c1') {
  const sse = setupSSE();
  const unsub = subscribeCaseStream(caseId);
  await flush();
  return { sse, unsub, fetchMock: sse.fetchMock as ReturnType<typeof vi.fn> };
}

describe('subscribeCaseStream', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    clearApiKey();
    useEventStore.getState().clearEvents();
    useAgentStore.getState().resetAgents();
    useCaseStore.setState({ case: null, cases: [], loading: false, error: null });
  });

  it('opens a fetch stream on /api/v1/cases/:id/stream', async () => {
    const { fetchMock, unsub, sse } = await subscribeReady();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/cases/c1/stream',
      expect.objectContaining({ headers: expect.objectContaining({ Accept: 'text/event-stream' }) }),
    );
    unsub();
    sse.close();
  });

  it('sends X-API-Key header when an API key is stored', async () => {
    localStorage.setItem('magi.apiKey', 'sk-sse');
    const { fetchMock, unsub, sse } = await subscribeReady();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/cases/c1/stream',
      expect.objectContaining({ headers: expect.objectContaining({ 'X-API-Key': 'sk-sse' }) }),
    );
    unsub();
    sse.close();
  });

  it('pushes mapped events into eventStore', async () => {
    const { sse, unsub } = await subscribeReady();
    sse.emit({ id: 'e1', type: 'VOTE_SUBMITTED', message: 'Votes submitted', timestamp: 't' });
    await flush();
    expect(useEventStore.getState().events).toHaveLength(1);
    expect(useEventStore.getState().events[0].type).toBe('VOTE_SUBMITTED');
    expect(useEventStore.getState().events[0].message).toBe('Votes submitted');
    unsub();
  });

  it('parses CRLF-terminated SSE frames', async () => {
    const { sse, unsub } = await subscribeReady();
    sse.emitRaw('id: 3\r\ndata: {"id":"e3","type":"VOTE_SUBMITTED","message":"m","timestamp":"t"}\r\n\r\n');
    await flush();
    expect(useEventStore.getState().events).toHaveLength(1);
    expect(useEventStore.getState().events[0].id).toBe('e3');
    unsub();
  });

  it('handles a CRLF terminator split across read chunks', async () => {
    const { sse, unsub } = await subscribeReady();
    sse.emitRaw('data: {"id":"e4","type":"AGENT_STARTED","message":"m","timestamp":"t"}\r\n');
    await flush();
    expect(useEventStore.getState().events).toHaveLength(0);
    sse.emitRaw('\r\n');
    await flush();
    expect(useEventStore.getState().events).toHaveLength(1);
    unsub();
  });

  it('patches agentStore status for agent-scoped events', async () => {
    const { sse, unsub } = await subscribeReady();
    sse.emit({ id: 'e1', type: 'AGENT_STARTED', agent_code: 'melchior', message: 'm', timestamp: 't' });
    await flush();
    expect(useAgentStore.getState().agents.melchior?.status).toBe('running');
    unsub();
  });

  it('marks agent completed on CASE_COMPLETED', async () => {
    const { sse, unsub } = await subscribeReady();
    sse.emit({ id: 'e1', type: 'CASE_COMPLETED', agent_code: 'melchior', message: 'done', timestamp: 't' });
    await flush();
    expect(useAgentStore.getState().agents.melchior?.status).toBe('completed');
    unsub();
  });

  it('incrementally adds evidence from EVIDENCE_CREATED', async () => {
    useAgentStore.getState().patchAgent('melchior', { status: 'running' });
    const { sse, unsub } = await subscribeReady();
    sse.emit({
      id: 'e1', type: 'EVIDENCE_CREATED', agent_code: 'melchior', message: 'm',
      payload: { evidence_id: 'EV-1', reliability: 0.9, tool_name: 'web_search' }, timestamp: 't',
    });
    await flush();
    expect(useAgentStore.getState().agents.melchior?.evidence).toHaveLength(1);
    expect(useAgentStore.getState().agents.melchior?.evidence[0].id).toBe('EV-1');
    unsub();
  });

  it('incrementally updates tool calls through the lifecycle', async () => {
    useAgentStore.getState().patchAgent('melchior', { status: 'running' });
    const { sse, unsub } = await subscribeReady();
    sse.emit({
      id: 'e1', type: 'TOOL_CALL_REQUESTED', agent_code: 'melchior', message: 'm',
      payload: { tool_call_id: 'call-1', tool_name: 'web_search', arguments: '{"q":"x"}' }, timestamp: 't',
    });
    await flush();
    sse.emit({
      id: 'e2', type: 'TOOL_CALL_COMPLETED', agent_code: 'melchior', message: 'm',
      payload: { tool_call_id: 'call-1', tool_name: 'web_search', duration_ms: 42, result: 'found' }, timestamp: 't',
    });
    await flush();
    const tc = useAgentStore.getState().agents.melchior?.toolCalls[0];
    expect(tc?.name).toBe('web_search');
    expect(tc?.params).toEqual({ q: 'x' });
    expect(tc?.result).toBe('found');
    expect(useAgentStore.getState().agents.melchior?.toolCalls).toHaveLength(1);
    unsub();
  });

  it('incrementally sets the agent vote from VOTE_SUBMITTED', async () => {
    useAgentStore.getState().patchAgent('casper', { status: 'running' });
    const { sse, unsub } = await subscribeReady();
    sse.emit({
      id: 'e1', type: 'VOTE_SUBMITTED', agent_code: 'casper', message: 'm',
      payload: { stance: 'reject', confidence: 94, reasoning: 'r' }, timestamp: 't',
    });
    await flush();
    expect(useAgentStore.getState().agents.casper?.vote?.stance).toBe('reject');
    expect(useAgentStore.getState().agents.casper?.vote?.confidence).toBe(94);
    unsub();
  });

  it('applies CASE_STATUS_CHANGED to caseStore without pushing to the timeline', async () => {
    useCaseStore.getState().loadCaseList([
      { id: 'c1', question: 'Q?', status: 'DRAFT', round: 0, createdAt: 't', pinned: false, archived: false },
    ]);
    const { sse, unsub } = await subscribeReady();
    sse.emit({
      id: 'e1', type: 'CASE_STATUS_CHANGED', message: 'status',
      payload: { status: 'DEBATING', round: 2 }, timestamp: 't',
    });
    await flush();
    expect(useCaseStore.getState().cases.find((c) => c.id === 'c1')?.status).toBe('DEBATING');
    expect(useEventStore.getState().events).toHaveLength(0);
    unsub();
  });

  it('aborts the fetch on unsubscribe', async () => {
    const { fetchMock, unsub, sse } = await subscribeReady();
    const signal = (fetchMock.mock.calls[0][1] as RequestInit).signal as AbortSignal;
    unsub();
    expect(signal.aborted).toBe(true);
    sse.close();
  });

  it('dispatches UNAUTHORIZED_EVENT and stops on a 401', async () => {
    const dispatched: string[] = [];
    const onEvent = (e: Event) => dispatched.push(e.type);
    window.addEventListener(UNAUTHORIZED_EVENT, onEvent);
    try {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({}) } as Response));
      subscribeCaseStream('c1');
      await flush();
      expect(dispatched).toContain(UNAUTHORIZED_EVENT);
    } finally {
      window.removeEventListener(UNAUTHORIZED_EVENT, onEvent);
    }
  });

  it('reconnects after EOF and resumes with Last-Event-ID', async () => {
    const controllers: ReadableStreamDefaultController<Uint8Array>[] = [];
    const fetchMock = vi.fn().mockImplementation(() => {
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controllers.push(c);
        },
      });
      return Promise.resolve({ ok: true, status: 200, body: stream } as Response);
    });
    vi.stubGlobal('fetch', fetchMock);

    const unsub = subscribeCaseStream('c1');
    await flush();
    controllers[0].enqueue(
      encoder.encode(
        'id: 7\ndata: {"id":"e7","type":"AGENT_STARTED","agent_code":"melchior","message":"m","timestamp":"t","seq":7}\n\n',
      ),
    );
    await flush();
    expect(useEventStore.getState().events).toHaveLength(1);

    // A graceful server/proxy close (EOF) must trigger a reconnect, not a
    // silent stop. Wait past the first backoff.
    controllers[0].close();
    await new Promise((r) => setTimeout(r, 700));
    await flush();

    expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2);
    const secondInit = fetchMock.mock.calls[1][1] as RequestInit;
    expect((secondInit.headers as Record<string, string>)['Last-Event-ID']).toBe('7');
    unsub();
  });

  it('does not treat the payload seq as a competing resume watermark', async () => {
    const controllers: ReadableStreamDefaultController<Uint8Array>[] = [];
    const fetchMock = vi.fn().mockImplementation(() => {
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controllers.push(c);
        },
      });
      return Promise.resolve({ ok: true, status: 200, body: stream } as Response);
    });
    vi.stubGlobal('fetch', fetchMock);

    const unsub = subscribeCaseStream('c1');
    await flush();
    // Payload carries seq=9 but the frame has no `id:`; only `id:` may advance
    // the resume cursor.
    controllers[0].enqueue(
      encoder.encode(
        'data: {"id":"e9","type":"AGENT_STARTED","agent_code":"melchior","message":"m","timestamp":"t","seq":9}\n\n',
      ),
    );
    await flush();
    controllers[0].close();
    await new Promise((r) => setTimeout(r, 700));
    await flush();

    const secondInit = fetchMock.mock.calls[1][1] as RequestInit;
    expect((secondInit.headers as Record<string, string>)['Last-Event-ID']).toBeUndefined();
    unsub();
  });
});
