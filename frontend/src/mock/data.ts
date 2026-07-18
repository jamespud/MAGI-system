import type { Case, CaseSummary, CaseStatus } from '@/types/case';
import type { AgentId, AgentSnapshot, AgentStatus } from '@/types/agent';
import type { EvidenceRecord } from '@/types/evidence';
import type { MagiEvent } from '@/types/event';

const SAMPLE_CASE_ID = 'case-001';

export function createMockCase(): Case {
  return {
    id: SAMPLE_CASE_ID,
    question: 'Should we migrate the Java backend to Rust?',
    background: 'Our backend currently runs on Java 17 with Spring Boot. The team has been evaluating Rust for performance improvements and memory safety. We need to decide whether to rewrite the core services in Rust or continue with the current Java stack.',
    constraints: [
      { label: 'Budget', value: '3 months engineering time' },
      { label: 'Deadline', value: 'Q4 2026' },
      { label: 'Team Size', value: '5 engineers' },
      { label: 'Priority', value: 'High' },
    ],
    status: 'INVESTIGATING' as CaseStatus,
    round: 1,
    consensus: null,
    confidence: 0,
    createdAt: '2026-07-18T10:00:00Z',
    updatedAt: '2026-07-18T10:05:00Z',
  };
}

export function createMockCaseList(): CaseSummary[] {
  return [
    { id: 'case-001', question: 'Should we migrate the Java backend to Rust?', status: 'RESOLVED', round: 2, createdAt: '2026-07-18T10:00:00Z', pinned: true },
    { id: 'case-002', question: 'Which cloud provider for ML workloads?', status: 'DEBATING', round: 1, createdAt: '2026-07-17T14:00:00Z', pinned: false },
    { id: 'case-003', question: 'Monorepo vs polyrepo for team of 15?', status: 'RESOLVED', round: 1, createdAt: '2026-07-16T09:00:00Z', pinned: false },
    { id: 'case-004', question: 'Should we adopt event sourcing?', status: 'INVESTIGATING', round: 1, createdAt: '2026-07-15T11:00:00Z', pinned: false },
    { id: 'case-005', question: 'GraphQL or REST for public API?', status: 'RESOLVED', round: 2, createdAt: '2026-07-14T08:00:00Z', pinned: false },
  ];
}

export function createMockAgents(): Record<AgentId, AgentSnapshot> {
  const base: Record<AgentId, AgentSnapshot> = {
    melchior: {
      agentId: 'melchior',
      status: 'completed' as AgentStatus,
      step: 8,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
    balthasar: {
      agentId: 'balthasar',
      status: 'completed' as AgentStatus,
      step: 10,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
    casper: {
      agentId: 'casper',
      status: 'running' as AgentStatus,
      step: 6,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
  };

  base.melchior.thought = 'Analyzing the migration question from a scientific perspective. Rust offers memory safety guarantees that eliminate entire classes of bugs. Benchmark data from comparable migrations shows 30-40% latency reduction. However, the team has 0 Rust experience — the learning curve is real.';
  base.melchior.toolCalls = [
    { name: 'web_search', params: { query: 'Java Spring Boot vs Rust Actix performance benchmarks 2025' }, result: 'Found 23 benchmarks. Average latency reduction: 38%. Memory usage: 4x lower.', timestamp: '2026-07-18T10:01:00Z' },
    { name: 'web_search', params: { query: 'Java to Rust migration case study enterprise' }, result: '3 major case studies found: Discord, Dropbox, Figma. All reported positive outcomes.', timestamp: '2026-07-18T10:02:00Z' },
    { name: 'web_search', params: { query: 'Rust developer availability market survey 2026' }, result: 'Rust developer pool growing 40% YoY. Still ~15% of Java pool.', timestamp: '2026-07-18T10:03:00Z' },
  ];
  base.melchior.evidence = [
    { id: 'EV-001', source: 'Web Search', reliability: 0.91 },
    { id: 'EV-002', source: 'Web Search', reliability: 0.85 },
    { id: 'EV-003', source: 'Web Search', reliability: 0.78 },
  ];
  base.melchior.claims = [
    { id: 'CL-001', text: 'Rust reduces backend latency by 30-40% compared to Java', supports: ['CL-003'], contradicts: [] },
    { id: 'CL-002', text: 'Rust memory safety eliminates null pointer and concurrency bugs', supports: ['CL-003'], contradicts: [] },
  ];
  base.melchior.vote = { stance: 'Approve', confidence: 78, reasoning: 'Performance gains are substantial and well-documented. The migration is technically sound.', dimensions: { Correctness: 92, Efficiency: 88, Risk: 45 } };

  base.balthasar.thought = 'Risk assessment underway. The team has no Rust experience — this is the primary risk factor. Migration cost estimates from comparable projects range from 6-12 months. Current Java system is stable. What is the failure mode if the migration goes over budget?';
  base.balthasar.toolCalls = [
    { name: 'web_search', params: { query: 'failed Java to Rust migration postmortem' }, result: 'Found 7 postmortems. Common failure mode: timeline underestimation, skill gap.', timestamp: '2026-07-18T10:01:00Z' },
    { name: 'web_search', params: { query: 'Rust migration cost overrun statistics' }, result: 'Average overrun: 40% of initial estimate. Top cause: learning curve.', timestamp: '2026-07-18T10:02:00Z' },
  ];
  base.balthasar.evidence = [
    { id: 'EV-004', source: 'Web Search', reliability: 0.82 },
    { id: 'EV-005', source: 'Web Search', reliability: 0.88 },
  ];
  base.balthasar.claims = [
    { id: 'CL-003', text: 'Migration is feasible and beneficial with proper planning', supports: [], contradicts: ['CL-004'] },
    { id: 'CL-004', text: 'Team skill gap makes the migration too risky within budget', supports: [], contradicts: ['CL-003'] },
  ];
  base.balthasar.vote = { stance: 'Reject', confidence: 72, reasoning: 'Risk of timeline overrun and team skill gap outweigh benefits. Recommend incremental Rust adoption instead of full migration.', dimensions: { Correctness: 70, Efficiency: 45, Risk: 85 } };

  base.casper.thought = 'Exploring opportunity space. Rust opens up WASM edge computing and embedded systems — entirely new product possibilities. What if we use this migration to also modernize the architecture toward event-driven? Could this be a competitive advantage?';
  base.casper.toolCalls = [
    { name: 'web_search', params: { query: 'Rust WASM edge computing production use cases 2026' }, result: 'Cloudflare Workers, Fastly Compute@Edge, and AWS Lambda all support Rust. Growing ecosystem.', timestamp: '2026-07-18T10:04:00Z' },
  ];
  base.casper.evidence = [
    { id: 'EV-006', source: 'Web Search', reliability: 0.80 },
  ];
  base.casper.claims = [
    { id: 'CL-005', text: 'Rust migration enables WASM edge computing expansion', supports: ['CL-003'], contradicts: [] },
  ];
  base.casper.vote = null;

  return base;
}

export function createMockEvidence(): EvidenceRecord[] {
  return [
    { id: 'EV-001', source: 'Web Search', url: 'https://benchmarks.example.com/java-vs-rust-2025', observation: 'Rust Actix-web outperformed Spring Boot by 38% avg latency. Memory footprint was 4x smaller. Tests conducted on 32-core ARM64 machines.', reliability: 0.91, collectedBy: 'melchior', timestamp: '2026-07-18T10:01:30Z' },
    { id: 'EV-002', source: 'Web Search', url: 'https://case-studies.example.com/discord-rust-migration', observation: 'Discord migrated Go→Rust for their media service. 10x latency improvement. Go was already faster than Java, so Java→Rust may see even bigger gains.', reliability: 0.85, collectedBy: 'melchior', timestamp: '2026-07-18T10:02:30Z' },
    { id: 'EV-003', source: 'Web Search', url: 'https://survey.example.com/rust-devs-2026', observation: 'Rust developer pool has grown 40% year-over-year. Still approximately 15% the size of the Java developer pool.', reliability: 0.78, collectedBy: 'melchior', timestamp: '2026-07-18T10:03:30Z' },
    { id: 'EV-004', source: 'Web Search', url: 'https://postmortems.example.com/rust-migration-failures', observation: '7 failed migration case studies analyzed. Primary failure modes: timeline underestimation (71%), insufficient Rust training (57%), attempting full rewrite instead of incremental (43%).', reliability: 0.82, collectedBy: 'balthasar', timestamp: '2026-07-18T10:01:45Z' },
    { id: 'EV-005', source: 'Web Search', url: 'https://reports.example.com/migration-cost-overruns', observation: 'Average Rust migration overrun was 40% above initial estimates. Teams with prior systems programming experience reported 15% overrun vs 60% for teams without.', reliability: 0.88, collectedBy: 'balthasar', timestamp: '2026-07-18T10:02:45Z' },
    { id: 'EV-006', source: 'Web Search', url: 'https://edge-computing.example.com/rust-wasm-2026', observation: 'Cloudflare Workers, Fastly, and AWS Lambda all support Rust for edge functions. WASM-based edge compute market growing at 65% CAGR.', reliability: 0.80, collectedBy: 'casper', timestamp: '2026-07-18T10:04:30Z' },
    { id: 'EV-007', source: 'Web Search', url: 'https://security.example.com/rust-cve-analysis', observation: 'Rust codebases have 70% fewer memory-safety CVEs compared to equivalent Java codebases. Borrow checker eliminates use-after-free, double-free, and data races at compile time.', reliability: 0.94, collectedBy: 'melchior', timestamp: '2026-07-18T10:05:00Z' },
    { id: 'EV-008', source: 'Web Search', url: 'https://surveys.example.com/rust-productivity', observation: 'Developer productivity in Rust reaches parity with Java after approximately 3-6 months of learning. Initial 2-month period shows 50% productivity dip.', reliability: 0.87, collectedBy: 'balthasar', timestamp: '2026-07-18T10:06:00Z' },
  ];
}

export function createMockEvents(): MagiEvent[] {
  return [
    { id: 'evt-001', type: 'ROUND_START', timestamp: '2026-07-18T10:00:00Z', message: 'Round 1 started' },
    { id: 'evt-002', type: 'TOOL_CALL', timestamp: '2026-07-18T10:01:00Z', agentId: 'melchior', message: 'Melchior called web_search: Java vs Rust benchmarks', data: { tool: 'web_search' } },
    { id: 'evt-003', type: 'TOOL_CALL', timestamp: '2026-07-18T10:01:00Z', agentId: 'balthasar', message: 'Balthasar called web_search: migration failure postmortems', data: { tool: 'web_search' } },
    { id: 'evt-004', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:01:30Z', agentId: 'melchior', message: 'EV-001 created by Melchior (reliability: 0.91)', data: { evidenceId: 'EV-001' } },
    { id: 'evt-005', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:01:45Z', agentId: 'balthasar', message: 'EV-004 created by Balthasar (reliability: 0.82)', data: { evidenceId: 'EV-004' } },
    { id: 'evt-006', type: 'TOOL_CALL', timestamp: '2026-07-18T10:02:00Z', agentId: 'melchior', message: 'Melchior called web_search: Rust migration case studies', data: { tool: 'web_search' } },
    { id: 'evt-007', type: 'TOOL_CALL', timestamp: '2026-07-18T10:02:00Z', agentId: 'balthasar', message: 'Balthasar called web_search: cost overrun statistics', data: { tool: 'web_search' } },
    { id: 'evt-008', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:02:30Z', agentId: 'melchior', message: 'EV-002 created by Melchior (reliability: 0.85)', data: { evidenceId: 'EV-002' } },
    { id: 'evt-009', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:02:45Z', agentId: 'balthasar', message: 'EV-005 created by Balthasar (reliability: 0.88)', data: { evidenceId: 'EV-005' } },
    { id: 'evt-010', type: 'VOTE_SUBMITTED', timestamp: '2026-07-18T10:05:00Z', agentId: 'melchior', message: 'Melchior voted APPROVE (confidence: 78%)', data: { stance: 'Approve', confidence: 78 } },
    { id: 'evt-011', type: 'VOTE_SUBMITTED', timestamp: '2026-07-18T10:05:30Z', agentId: 'balthasar', message: 'Balthasar voted REJECT (confidence: 72%)', data: { stance: 'Reject', confidence: 72 } },
    { id: 'evt-012', type: 'CONSENSUS_CHANGED', timestamp: '2026-07-18T10:05:30Z', message: 'Consensus: 1:1 (Casper pending)', data: { approve: 1, reject: 1 } },
    { id: 'evt-013', type: 'DEBATE_START', timestamp: '2026-07-18T10:06:00Z', message: 'Debate initiated between Melchior and Balthasar', data: { claims: ['CL-003', 'CL-004'] } },
    { id: 'evt-014', type: 'AGENT_STEP', timestamp: '2026-07-18T10:06:30Z', agentId: 'casper', message: 'Casper step 5: searching for WASM edge computing use cases', data: { step: 5 } },
    { id: 'evt-015', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:04:30Z', agentId: 'casper', message: 'EV-006 created by Casper (reliability: 0.80)', data: { evidenceId: 'EV-006' } },
  ];
}
