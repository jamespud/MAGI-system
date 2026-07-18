import type { AgentId } from '@/types/agent';
import { AGENT_NAMES } from '@/types/agent';

interface AgentAvatarProps {
  agentId: AgentId;
  size?: number;
}

export default function AgentAvatar({ agentId, size = 24 }: AgentAvatarProps) {
  const colors: Record<AgentId, string> = {
    melchior: 'var(--melchior)',
    balthasar: 'var(--balthasar)',
    casper: 'var(--casper)',
  };

  return (
    <span
      className="inline-flex items-center justify-center rounded-full font-mono text-xs font-bold"
      style={{
        width: size,
        height: size,
        backgroundColor: `${colors[agentId]}20`,
        color: colors[agentId],
        border: `1px solid ${colors[agentId]}40`,
      }}
    >
      {AGENT_NAMES[agentId][0]}
    </span>
  );
}
