import { useEffect, useRef } from 'react';
import * as d3 from 'd3';
import { useUiStore } from '@/stores';
import { createMockEvidence, createMockAgents } from '@/mock/data';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  label: string;
  type: 'evidence' | 'claim' | 'vote';
  color: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  type: 'supports' | 'contradicts';
}

export default function EvidenceGraph() {
  const svgRef = useRef<SVGSVGElement>(null);
  const select = useUiStore((s) => s.select);

  useEffect(() => {
    const evidence = createMockEvidence();
    const agents = createMockAgents();

    const nodes: GraphNode[] = [
      ...evidence.map((e) => ({
        id: e.id,
        label: e.id,
        type: 'evidence' as const,
        color: e.collectedBy === 'melchior' ? 'var(--melchior)'
             : e.collectedBy === 'balthasar' ? 'var(--balthasar)'
             : 'var(--casper)',
      })),
      ...Object.values(agents).flatMap((a) =>
        (a.claims || []).map((c) => ({
          id: c.id,
          label: c.text.slice(0, 30) + (c.text.length > 30 ? '...' : ''),
          type: 'claim' as const,
          color: a.agentId === 'melchior' ? 'var(--melchior)'
               : a.agentId === 'balthasar' ? 'var(--balthasar)'
               : 'var(--casper)',
        }))
      ),
      ...Object.entries(agents)
        .filter(([, a]) => a.vote)
        .map(([id, a]) => ({
          id: `vote-${id}`,
          label: `${id}: ${a.vote!.stance}`,
          type: 'vote' as const,
          color: a.vote!.stance === 'Approve' ? 'var(--accent)' : 'var(--error)',
        })),
    ];

    const links: GraphLink[] = [
      { source: 'EV-001', target: 'CL-001', type: 'supports' },
      { source: 'EV-002', target: 'CL-001', type: 'supports' },
      { source: 'EV-003', target: 'CL-002', type: 'supports' },
      { source: 'EV-004', target: 'CL-004', type: 'supports' },
      { source: 'EV-005', target: 'CL-004', type: 'supports' },
      { source: 'EV-006', target: 'CL-005', type: 'supports' },
      { source: 'EV-007', target: 'CL-002', type: 'supports' },
      { source: 'EV-008', target: 'CL-004', type: 'supports' },
      { source: 'CL-001', target: 'CL-003', type: 'supports' },
      { source: 'CL-002', target: 'CL-003', type: 'supports' },
      { source: 'CL-003', target: 'CL-004', type: 'contradicts' },
      { source: 'CL-005', target: 'CL-003', type: 'supports' },
      { source: 'CL-001', target: 'vote-melchior', type: 'supports' },
      { source: 'CL-004', target: 'vote-balthasar', type: 'supports' },
    ];

    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    const width = svgRef.current?.clientWidth || 800;
    const height = 320;

    svg.attr('viewBox', `0 0 ${width} ${height}`);

    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(links).id((d) => d.id).distance(80))
      .force('charge', d3.forceManyBody().strength(-200))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide(20));

    const link = svg.append('g')
      .selectAll('line')
      .data(links)
      .join('line')
      .attr('stroke', (d) => d.type === 'contradicts' ? 'var(--error)' : 'var(--border-dim)')
      .attr('stroke-width', 1.5)
      .attr('stroke-dasharray', (d) => d.type === 'contradicts' ? '4 2' : 'none')
      .attr('opacity', 0.6);

    const node = svg.append('g')
      .selectAll('g')
      .data(nodes)
      .join('g')
      .style('cursor', 'pointer')
      .on('click', (_e, d) => {
        select({ type: d.type === 'vote' ? 'vote' : 'evidence', id: d.id });
      });

    node.append('circle')
      .attr('r', (d) => d.type === 'evidence' ? 8 : d.type === 'vote' ? 12 : 10)
      .attr('fill', (d) => d.type === 'evidence' ? 'var(--bg-raised)' : d.color + '20')
      .attr('stroke', (d) => d.color)
      .attr('stroke-width', (d) => d.type === 'vote' ? 2 : 1.5);

    node.append('text')
      .text((d) => d.label)
      .attr('x', 14)
      .attr('y', 4)
      .attr('fill', 'var(--text-secondary)')
      .attr('font-size', '9px')
      .attr('font-family', 'JetBrains Mono, monospace');

    const dragBehavior = d3.drag<SVGGElement, GraphNode>()
      .on('start', (_event, d) => {
        simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on('drag', (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on('end', (_event, d) => {
        simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      });

    node.call(dragBehavior as any);

    simulation.on('tick', () => {
      link
        .attr('x1', (d) => (d.source as GraphNode).x!)
        .attr('y1', (d) => (d.source as GraphNode).y!)
        .attr('x2', (d) => (d.target as GraphNode).x!)
        .attr('y2', (d) => (d.target as GraphNode).y!);

      node.attr('transform', (d) => `translate(${d.x},${d.y})`);
    });

    return () => { simulation.stop(); };
  }, [select]);

  return (
    <Card className="mx-4 mb-4" padded={false}>
      <div className="px-4 pt-3 pb-2 border-b border-border-dim">
        <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">Evidence Graph</h3>
        <div className="flex items-center gap-3 mt-1">
          <div className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full bg-border-dim" />
            <MonoText size="sm" muted>Supports</MonoText>
          </div>
          <div className="flex items-center gap-1">
            <span className="inline-block bg-error" style={{ width: 8, height: 2 }} />
            <MonoText size="sm" muted>Contradicts</MonoText>
          </div>
        </div>
      </div>
      <svg ref={svgRef} className="w-full cursor-grab active:cursor-grabbing" style={{ height: 320 }} />
    </Card>
  );
}
