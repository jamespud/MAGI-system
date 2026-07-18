import AgentPanel from './AgentPanel';

export default function AgentTrio() {
  return (
    <div className="flex gap-3 p-4">
      <AgentPanel agentId="melchior" />
      <AgentPanel agentId="balthasar" />
      <AgentPanel agentId="casper" />
    </div>
  );
}
