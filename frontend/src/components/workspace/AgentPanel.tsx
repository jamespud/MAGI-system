import type { AgentId } from '@/types/agent';
import { AGENT_NAMES, AGENT_ROLES, AGENT_COLORS } from '@/types/agent';
import { useAgentStore, useUiStore } from '@/stores';
import { GlowBorder, ScanLine, StatusBadge, MonoText } from '@/components/shared';
import { Card } from '@/components/ui';
import { Wrench, FileSearch, Lightbulb, Vote } from 'lucide-react';

interface AgentPanelProps {
  agentId: AgentId;
}

export default function AgentPanel({ agentId }: AgentPanelProps) {
  const agent = useAgentStore((s) => s.agents[agentId]);
  const expandedAgent = useUiStore((s) => s.expandedAgent);
  const setExpandedAgent = useUiStore((s) => s.setExpandedAgent);
  const color = AGENT_COLORS[agentId];
  const isExpanded = expandedAgent === agentId;

  const handleClick = () => {
    setExpandedAgent(isExpanded ? null : agentId);
  };

  if (!agent) {
    return (
      <Card className="flex-1 min-w-0">
        <div className="text-center py-8">
          <MonoText muted>{AGENT_NAMES[agentId]}</MonoText>
          <div className="mt-2">
            <span className="font-mono text-xs text-text-muted">Waiting for agent data...</span>
          </div>
        </div>
      </Card>
    );
  }

  const isRunning = agent.status === 'running';

  return (
    <GlowBorder
      color={color}
      active={isRunning}
      className={`flex-1 min-w-0 cursor-pointer transition-all duration-300 overflow-hidden relative ${isExpanded ? 'flex-[2]' : 'flex-1'}`}
    >
      <div onClick={handleClick} className="h-full" role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter') handleClick(); }}>
        {isRunning && <ScanLine />}

        <div className="px-4 pt-3 pb-2 border-b border-border-dim">
          <div className="flex items-center justify-between mb-1">
            <h3 className="font-mono text-sm font-bold tracking-wider" style={{ color }}>
              {AGENT_NAMES[agentId]}
            </h3>
            <StatusBadge
              status={agent.status.toUpperCase()}
              color={isRunning ? color : undefined}
            />
          </div>
          <p className="text-xs text-text-muted mb-2">{AGENT_ROLES[agentId]}</p>

          <div className="flex items-center gap-2 mb-1">
            <div className="flex-1 h-1 bg-border-dim rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{
                  width: `${(agent.step / agent.maxSteps) * 100}%`,
                  backgroundColor: color,
                }}
              />
            </div>
            <MonoText size="sm">{agent.step}/{agent.maxSteps}</MonoText>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 px-4 py-3">
          <div className="flex items-center gap-1.5">
            <Wrench size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.toolCalls.length} Tools</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <FileSearch size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.evidence.length} Evidence</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <Lightbulb size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.claims.length} Claims</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <Vote size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.vote?.stance || 'Pending'}</MonoText>
          </div>
        </div>

        {isExpanded && (
          <div className="px-4 pb-4 space-y-3 animate-fade-in border-t border-border-dim pt-3">
            {agent.thought && (
              <div>
                <MonoText size="sm" muted>Thought</MonoText>
                <p className="text-xs text-text-secondary mt-1 leading-relaxed">{agent.thought}</p>
              </div>
            )}

            {agent.toolCalls.length > 0 && (
              <div>
                <MonoText size="sm" muted>Tool Calls</MonoText>
                <div className="mt-1 space-y-1.5">
                  {agent.toolCalls.map((tc, i) => (
                    <div key={i} className="bg-raised rounded p-2 text-xs">
                      <div className="font-mono" style={{ color }}>{tc.name}({JSON.stringify(tc.params)})</div>
                      {tc.result && <div className="text-text-muted mt-0.5 truncate">{tc.result}</div>}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {agent.evidence.length > 0 && (
              <div>
                <MonoText size="sm" muted>Evidence</MonoText>
                <div className="flex flex-wrap gap-1 mt-1">
                  {agent.evidence.map((ev) => (
                    <span
                      key={ev.id}
                      className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-raised text-text-secondary border border-border-dim cursor-pointer hover:border-[var(--border-active)]"
                    >
                      {ev.id}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {agent.claims.length > 0 && (
              <div>
                <MonoText size="sm" muted>Claims</MonoText>
                <ul className="mt-1 space-y-1">
                  {agent.claims.map((cl) => (
                    <li key={cl.id} className="text-xs text-text-secondary pl-2 border-l-2" style={{ borderColor: `${color}60` }}>
                      {cl.text}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {agent.vote && (
              <div>
                <MonoText size="sm" muted>Vote</MonoText>
                <div className="mt-1 p-2 rounded bg-raised">
                  <div className="flex items-center gap-2 mb-1">
                    <span className={`font-mono text-xs font-bold ${agent.vote.stance === 'Approve' ? 'text-accent' : 'text-error'}`}>
                      {agent.vote.stance.toUpperCase()}
                    </span>
                    <MonoText size="sm" muted>{agent.vote.confidence}% confidence</MonoText>
                  </div>
                  <p className="text-xs text-text-secondary leading-relaxed">{agent.vote.reasoning}</p>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </GlowBorder>
  );
}
