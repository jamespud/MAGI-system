import { describe, it, expect } from 'vitest';
import { formatEventMessage, sortEventsNewestFirst } from '../BottomTimeline';
import type { MagiEvent } from '@/types/event';

function ev(type: MagiEvent['type'], data?: Record<string, unknown>, message = 'fallback'): MagiEvent {
  return { id: 'e1', type, timestamp: 't', message, data };
}

describe('formatEventMessage', () => {
  it('uses tool_name for TOOL_CALL', () => {
    expect(formatEventMessage(ev('TOOL_CALL', { tool_name: 'web_search' }))).toBe('called web_search');
  });

  it('uses evidence_id for EVIDENCE_CREATED', () => {
    expect(formatEventMessage(ev('EVIDENCE_CREATED', { evidence_id: 'EV-001' }))).toBe('evidence EV-001');
  });

  it('uses stance + confidence for VOTE_SUBMITTED', () => {
    expect(formatEventMessage(ev('VOTE_SUBMITTED', { stance: 'approve', confidence: 80 }))).toBe('voted approve (80%)');
  });

  it('uses outcome for CONSENSUS_CHANGED', () => {
    expect(formatEventMessage(ev('CONSENSUS_CHANGED', { outcome: 'strong_approval' }))).toBe('consensus: strong_approval');
  });

  it('falls back to event.message when payload lacks the field', () => {
    expect(formatEventMessage(ev('TOOL_CALL', {}, 'Tool call requested'))).toBe('Tool call requested');
  });

  it('falls back to event.message for types without payload logic', () => {
    expect(formatEventMessage(ev('ROUND_START', undefined, 'Round started'))).toBe('Round started');
  });
});

describe('sortEventsNewestFirst', () => {
  it('orders events newest first', () => {
    const older = { id: 'e1', type: 'AGENT_STEP' as const, timestamp: '2026-01-01T00:00:00Z', message: 'older' };
    const newer = { id: 'e2', type: 'AGENT_STEP' as const, timestamp: '2026-01-02T00:00:00Z', message: 'newer' };
    expect(sortEventsNewestFirst([older, newer]).map((e) => e.id)).toEqual(['e2', 'e1']);
  });

  it('does not mutate the input array', () => {
    const a = { id: 'e1', type: 'AGENT_STEP' as const, timestamp: '2026-01-01T00:00:00Z', message: 'm' };
    const b = { id: 'e2', type: 'AGENT_STEP' as const, timestamp: '2026-01-02T00:00:00Z', message: 'm' };
    const input = [a, b];
    sortEventsNewestFirst(input);
    expect(input).toEqual([a, b]);
  });
});
