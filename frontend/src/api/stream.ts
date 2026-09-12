import { useCaseStore, useEventStore, useAgentStore } from '@/stores';
import { mapBackendEvent } from './eventMapper';
import { getApiKey, UNAUTHORIZED_EVENT, type ApiEvent } from './client';
import type { AgentId, AgentStatus } from '@/types/agent';
import type { CaseStatus } from '@/types/case';
import { normalizeStance } from '@/lib/stance';

// Maps backend event types to the agent status they imply. Absent types
// (e.g. TASK_NORMALIZED) leave the agent status untouched.
const STATUS_BY_TYPE: Record<string, AgentStatus> = {
  AGENT_STARTED: 'running',
  MODEL_REQUESTED: 'running',
  MODEL_RESPONDED: 'running',
  TOOL_CALL_REQUESTED: 'running',
  TOOL_CALL_STARTED: 'running',
  EVIDENCE_CREATED: 'running',
  VOTE_SUBMITTED: 'completed',
  CASE_COMPLETED: 'completed',
  CASE_FAILED: 'error',
  EVIDENCE_GATE_FAILED: 'error',
};

// Terminal backend event types: when these arrive the run is over and the
// caller should re-fetch the case + artifacts (status/consensus/votes are
// only final after completion).
const TERMINAL_TYPES = new Set(['CASE_COMPLETED', 'CASE_FAILED']);

function parseArgs(args: unknown): Record<string, string> {
  if (typeof args !== 'string') return {};
  try {
    const parsed = JSON.parse(args);
    if (parsed && typeof parsed === 'object') {
      const out: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed)) out[k] = String(v);
      return out;
    }
  } catch {
    // not JSON
  }
  return {};
}

// applyIncremental updates the agent snapshot from live SSE events, so the
// agent panel and evidence graph reflect evidence/tool calls/votes as they
// happen instead of waiting for the terminal refetch.
function applyIncremental(raw: ApiEvent) {
  if (!raw.agent_code) return;
  const agentId = raw.agent_code as AgentId;
  const p = raw.payload ?? {};
  const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined);
  const num = (v: unknown): number | undefined => (typeof v === 'number' ? v : undefined);
  switch (raw.type) {
    case 'MODEL_RESPONDED': {
      const step = num(p.step);
      if (step != null) useAgentStore.getState().patchAgent(agentId, { step });
      break;
    }
    case 'EVIDENCE_CREATED': {
      const id = str(p.evidence_id);
      if (id) useAgentStore.getState().addEvidence(agentId, { id, source: str(p.tool_name) ?? '', reliability: num(p.reliability) ?? 0 });
      break;
    }
    case 'CLAIM_CREATED': {
      const stmt = str(p.statement);
      if (stmt) useAgentStore.getState().addClaim(agentId, {
        id: str(p.claim_id) ?? '',
        text: stmt,
        supports: Array.isArray(p.supports) ? p.supports as string[] : [],
        contradicts: Array.isArray(p.contradicts) ? p.contradicts as string[] : [],
      });
      break;
    }
    case 'TOOL_CALL_REQUESTED': {
      const id = str(p.tool_call_id);
      const name = str(p.tool_name);
      if (id && name) useAgentStore.getState().upsertToolCall(agentId, { id, name, params: parseArgs(p.arguments), result: null });
      break;
    }
    case 'TOOL_CALL_STARTED': {
      const id = str(p.tool_call_id);
      const name = str(p.tool_name);
      if (id && name) useAgentStore.getState().upsertToolCall(agentId, { id, name });
      break;
    }
    case 'TOOL_CALL_COMPLETED': {
      const id = str(p.tool_call_id);
      const name = str(p.tool_name);
      if (id && name) useAgentStore.getState().upsertToolCall(agentId, { id, name, result: str(p.result) ?? '', durationMs: num(p.duration_ms) });
      break;
    }
    case 'TOOL_CALL_FAILED': {
      const id = str(p.tool_call_id);
      const name = str(p.tool_name);
      if (id && name) useAgentStore.getState().upsertToolCall(agentId, { id, name, error: str(p.error) ?? 'tool failed' });
      break;
    }
    case 'VOTE_SUBMITTED': {
      const stance = str(p.stance);
      if (stance) useAgentStore.getState().setVote(agentId, {
        stance: normalizeStance(stance),
        confidence: num(p.confidence) ?? 0,
        reasoning: str(p.reasoning) ?? '',
      });
      break;
    }
  }
}

// subscribeCaseStream opens an SSE connection to /cases/:id/stream, maps each
// backend event to the frontend shape, pushes it into eventStore, and patches
// agentStore status for agent-scoped events. onTerminal (optional) is called
// once when a terminal event (CASE_COMPLETED/CASE_FAILED) arrives so the caller
// can refresh case detail + artifacts. Returns an unsubscribe that closes the
// connection.
//
// SSE is consumed over fetch (ReadableStream) instead of EventSource so the
// API key can be sent as the X-API-Key header under multi-tenant auth — the
// browser EventSource API cannot set request headers (P0: D1). Network errors
// retry with backoff like EventSource; a 401 stops and routes to /login.
export function subscribeCaseStream(caseId: string, onTerminal?: () => void): () => void {
  const controller = new AbortController();
  let closed = false;
  let retryMs = 500;
  // lastEventId is the resume cursor (only the SSE `id:` field writes it);
  // lastPayloadSeq is the gap checker (only the JSON payload seq writes it).
  // Keeping them separate is what makes gap detection actually fire when
  // id == seq on the wire.
  let lastEventId = '';
  let lastPayloadSeq = 0;

  const processEvent = (data: string) => {
    let raw: ApiEvent;
    try {
      raw = JSON.parse(data) as ApiEvent;
    } catch {
      return; // ignore malformed frames
    }
    // Sequence gaps mean the broker dropped frames for a slow consumer;
    // refetch the authoritative state instead of rendering a hole.
    if (typeof raw.seq === 'number' && raw.seq > 0) {
      if (lastPayloadSeq > 0 && raw.seq > lastPayloadSeq + 1) {
        void useCaseStore.getState().fetchCase(caseId, { silent: true });
        if (onTerminal) onTerminal(); // refetch artifacts as well
      }
      lastPayloadSeq = Math.max(lastPayloadSeq, raw.seq);
    }
    if (raw.type === 'CASE_STATUS_CHANGED') {
      const status = raw.payload?.status;
      const round = raw.payload?.round;
      if (typeof status === 'string') {
        useCaseStore.getState().updateCaseStatus(caseId, status as CaseStatus, typeof round === 'number' ? round : 0);
      }
      return; // phase transitions update the case status, not the timeline
    }
    applyIncremental(raw);
    const ev = mapBackendEvent(raw);
    useEventStore.getState().pushEvent(ev);
    if (ev.agentId) {
      const status = STATUS_BY_TYPE[raw.type];
      if (status) {
        // patchAgent (not updateAgentStatus) so a snapshot is created when the
        // agent is first seen mid-stream rather than left null.
        useAgentStore.getState().patchAgent(ev.agentId as AgentId, { status });
      }
    }
    if (onTerminal && TERMINAL_TYPES.has(raw.type)) {
      onTerminal();
    }
  };

  // findFrameEnd locates the next frame terminator, accepting both LF (the
  // common case) and CRLF (some servers/proxies terminate frames with
  // `\r\n\r\n`), whichever appears first.
  const findFrameEnd = (buf: string): { idx: number; len: number } | null => {
    const lf = buf.indexOf('\n\n');
    const crlf = buf.indexOf('\r\n\r\n');
    if (crlf !== -1 && (lf === -1 || crlf < lf)) return { idx: crlf, len: 4 };
    if (lf !== -1) return { idx: lf, len: 2 };
    return null;
  };

  // processFrame parses one SSE frame per the wire format: `id:` advances the
  // resume cursor and `data:` lines are concatenated. Ignoring `id:` (as the
  // previous parser did) means a reconnect replays the entire history.
  const processFrame = (frame: string) => {
    const dataLines: string[] = [];
    for (const line of frame.split(/\r?\n/)) {
      if (line.startsWith('id:')) {
        const id = line.slice(3).trim();
        if (id) lastEventId = id;
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (dataLines.length) processEvent(dataLines.join('\n'));
  };

  // scheduleReconnect retries the stream after a backoff. Every non-deliberate
  // termination (network error *and* graceful EOF) funnels through here so a
  // proxy close cannot silently end the feed.
  const scheduleReconnect = () => {
    if (closed || controller.signal.aborted) return;
    const delay = retryMs;
    retryMs = Math.min(retryMs * 2, 10_000);
    setTimeout(() => {
      if (!closed && !controller.signal.aborted) void connect();
    }, delay);
  };

  const connect = async () => {
    const headers: Record<string, string> = { Accept: 'text/event-stream' };
    const key = getApiKey();
    if (key) headers['X-API-Key'] = key;
    if (lastEventId) headers['Last-Event-ID'] = lastEventId;
    try {
      const res = await fetch(`/api/v1/cases/${caseId}/stream`, { headers, signal: controller.signal });
      if (res.status === 401) {
        // Bad/expired key: surface the auth problem and stop reconnecting.
        if (!closed) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
        return;
      }
      if (!res.ok || !res.body) throw new Error(`SSE HTTP ${res.status}`);
      retryMs = 500; // successful connect resets backoff
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done || closed || controller.signal.aborted) break;
        buf += decoder.decode(value, { stream: true });
        let end: { idx: number; len: number } | null;
        while ((end = findFrameEnd(buf)) !== null) {
          const frame = buf.slice(0, end.idx);
          buf = buf.slice(end.idx + end.len);
          processFrame(frame);
        }
      }
      // EOF from a graceful close still needs a reconnect; only an explicit
      // unsubscribe (closed) suppresses it.
      scheduleReconnect();
    } catch (e) {
      if (closed || controller.signal.aborted) return;
      if (e instanceof DOMException && e.name === 'AbortError') return;
      scheduleReconnect();
    }
  };

  void connect();
  return () => {
    closed = true;
    controller.abort();
  };
}
