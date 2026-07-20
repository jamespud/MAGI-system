import { describe, it, expect } from 'vitest';
import { mapBackendEvent } from './eventMapper';
import type { ApiEvent } from './client';

function ev(partial: Partial<ApiEvent>): ApiEvent {
  return { id: 'e1', type: 'VOTE_SUBMITTED', message: 'm', timestamp: 't', ...partial };
}

describe('mapBackendEvent', () => {
  it('maps TOOL_CALL_REQUESTED to TOOL_CALL and keeps agentId', () => {
    const out = mapBackendEvent(ev({ id: 'e1', type: 'TOOL_CALL_REQUESTED', agent_code: 'melchior' }));
    expect(out.type).toBe('TOOL_CALL');
    expect(out.agentId).toBe('melchior');
    expect(out.id).toBe('e1');
  });

  it('maps VOTE_SUBMITTED and keeps payload', () => {
    const out = mapBackendEvent(ev({ id: 'e2', type: 'VOTE_SUBMITTED', payload: { round: 1 } }));
    expect(out.type).toBe('VOTE_SUBMITTED');
    expect(out.data?.round).toBe(1);
  });

  it('maps DEBATE_STARTED to DEBATE_START', () => {
    const out = mapBackendEvent(ev({ type: 'DEBATE_STARTED' }));
    expect(out.type).toBe('DEBATE_START');
  });

  it('maps CONSENSUS_EVALUATED to CONSENSUS_CHANGED', () => {
    const out = mapBackendEvent(ev({ type: 'CONSENSUS_EVALUATED' }));
    expect(out.type).toBe('CONSENSUS_CHANGED');
  });

  it('maps AGENT_STARTED to AGENT_STEP', () => {
    const out = mapBackendEvent(ev({ type: 'AGENT_STARTED' }));
    expect(out.type).toBe('AGENT_STEP');
  });

  it('maps CASE_CREATED to ROUND_START', () => {
    const out = mapBackendEvent(ev({ type: 'CASE_CREATED' }));
    expect(out.type).toBe('ROUND_START');
  });

  it('maps RESOLUTION_CREATED and CASE_COMPLETED to RESOLVED', () => {
    expect(mapBackendEvent(ev({ type: 'RESOLUTION_CREATED' })).type).toBe('RESOLVED');
    expect(mapBackendEvent(ev({ type: 'CASE_COMPLETED' })).type).toBe('RESOLVED');
  });

  it('maps CASE_FAILED to ERROR', () => {
    expect(mapBackendEvent(ev({ type: 'CASE_FAILED' })).type).toBe('ERROR');
    expect(mapBackendEvent(ev({ type: 'EVIDENCE_GATE_FAILED' })).type).toBe('ERROR');
  });

  it('falls back to ERROR for unknown types', () => {
    const out = mapBackendEvent(ev({ type: 'SOMETHING_NEW' }));
    expect(out.type).toBe('ERROR');
  });

  it('preserves message and timestamp', () => {
    const out = mapBackendEvent(ev({ message: 'Votes submitted', timestamp: '2026-01-01T00:00:00Z' }));
    expect(out.message).toBe('Votes submitted');
    expect(out.timestamp).toBe('2026-01-01T00:00:00Z');
  });
});
