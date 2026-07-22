import { useEffect, useRef, useState } from 'react';
import * as d3 from 'd3';
import { useParams } from 'react-router-dom';
import { ZoomIn, ZoomOut, Maximize2 } from 'lucide-react';
import { useUiStore } from '@/stores';
import { api } from '@/api/client';
import type { ApiEvidence, ApiClaim, ApiVote } from '@/api/client';
import { stanceColor } from '@/lib/stance';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';

const HEIGHT = 520;

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  label: string;
  type: 'evidence' | 'claim' | 'vote';
  color: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  type: 'supports' | 'contradicts';
}

function agentColor(code: string): string {
  return code === 'melchior' ? 'var(--melchior)'
    : code === 'balthasar' ? 'var(--balthasar)'
    : 'var(--casper)';
}

function buildGraph(evidence: ApiEvidence[], claims: ApiClaim[], votes: ApiVote[]) {
  const nodes: GraphNode[] = [
    ...evidence.map((e) => ({
      id: e.id, label: e.id, type: 'evidence' as const, color: agentColor(e.collected_by),
    })),
    ...claims.map((c) => ({
      id: c.id,
      label: c.text.slice(0, 30) + (c.text.length > 30 ? '...' : ''),
      type: 'claim' as const, color: agentColor(c.created_by),
    })),
    ...votes.map((v) => ({
      id: `vote-${v.agent_code}`,
      label: `${v.agent_code}: ${v.stance}`,
      type: 'vote' as const, color: stanceColor(v.stance),
    })),
  ];

  const links: GraphLink[] = [];
  const nodeIds = new Set(nodes.map((n) => n.id));
  for (const c of claims) {
    for (const evId of c.supports) {
      if (nodeIds.has(evId)) links.push({ source: evId, target: c.id, type: 'supports' });
    }
    for (const cId of c.contradicts) {
      if (nodeIds.has(cId)) links.push({ source: cId, target: c.id, type: 'contradicts' });
    }
    const voteId = `vote-${c.created_by}`;
    if (nodeIds.has(voteId)) links.push({ source: c.id, target: voteId, type: 'supports' });
  }
  // Orphan evidence (not referenced by any claim's supports) links directly
  // to its collector agent's vote, so no evidence is an isolated node. This
  // yields a tree per agent (vote <- claims <- evidence) instead of a mesh.
  const referencedEv = new Set<string>();
  for (const c of claims) {
    for (const evId of c.supports) referencedEv.add(evId);
  }
  for (const e of evidence) {
    if (referencedEv.has(e.id)) continue;
    const voteId = `vote-${e.collected_by}`;
    if (nodeIds.has(voteId)) {
      links.push({ source: e.id, target: voteId, type: 'supports' });
    }
  }
  return { nodes, links };
}

export default function EvidenceGraph() {
  const { caseId } = useParams<{ caseId: string }>();
  const svgRef = useRef<SVGSVGElement>(null);
  const zoomRef = useRef<d3.ZoomBehavior<SVGSVGElement, unknown> | null>(null);
  const [empty, setEmpty] = useState(false);
  const [zoomPct, setZoomPct] = useState(100);

  useEffect(() => {
    if (!caseId || !svgRef.current) return;

    let cancelled = false;
    setZoomPct(100);
    Promise.all([
      api.getEvidence(caseId).catch(() => [] as ApiEvidence[]),
      api.getClaims(caseId).catch(() => [] as ApiClaim[]),
      api.getVotes(caseId).catch(() => [] as ApiVote[]),
    ]).then(([evidence, claims, votes]) => {
      if (cancelled) return;
      const { nodes, links } = buildGraph(evidence, claims, votes);
      setEmpty(nodes.length === 0);
      if (nodes.length === 0 || !svgRef.current) return;

      const svg = d3.select(svgRef.current);
      svg.selectAll('*').remove();

      const width = svgRef.current.clientWidth || 800;
      svg.attr('viewBox', `0 0 ${width} ${HEIGHT}`);

      // All rendered content lives in a zoom layer so one transform scales/pans everything.
      const zoomLayer = svg.append('g');

      const zoom = d3.zoom<SVGSVGElement, unknown>()
        .scaleExtent([0.3, 3])
        .on('zoom', (e) => {
          zoomLayer.attr('transform', e.transform);
          setZoomPct(Math.round(e.transform.k * 100));
        });
      svg.call(zoom);
      zoomRef.current = zoom;

      const simulation = d3.forceSimulation<GraphNode>(nodes)
        .force('link', d3.forceLink<GraphNode, GraphLink>(links).id((d) => d.id).distance(80))
        .force('charge', d3.forceManyBody().strength(-200))
        .force('center', d3.forceCenter(width / 2, HEIGHT / 2))
        .force('collision', d3.forceCollide(20));

      const link = zoomLayer.append('g')
        .selectAll('line')
        .data(links)
        .join('line')
        .attr('stroke', (d) => d.type === 'contradicts' ? 'var(--error)' : 'var(--border-dim)')
        .attr('stroke-width', 1.5)
        .attr('stroke-dasharray', (d) => d.type === 'contradicts' ? '4 2' : 'none')
        .attr('opacity', 0.6);

      const node = zoomLayer.append('g')
        .selectAll('g')
        .data(nodes)
        .join('g')
        .style('cursor', 'pointer')
        .on('click', (_e, d) => {
          useUiStore.getState().select({ type: d.type === 'vote' ? 'vote' : 'evidence', id: d.id });
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

      // stopPropagation on drag so panning (zoom) doesn't also fire while dragging a node.
      const dragBehavior = d3.drag<SVGGElement, GraphNode>()
        .on('start', (event, d) => {
          event.sourceEvent?.stopPropagation();
          simulation.alphaTarget(0.3).restart();
          d.fx = d.x; d.fy = d.y;
        })
        .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y; })
        .on('end', (event, d) => {
          event.sourceEvent?.stopPropagation();
          simulation.alphaTarget(0);
          d.fx = null; d.fy = null;
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

      cancelled = true;
      return () => { simulation.stop(); };
    });

    return () => { cancelled = true; };
  }, [caseId]);

  const zoomBy = (factor: number) => {
    const z = zoomRef.current;
    if (!z || !svgRef.current) return;
    z.scaleBy(d3.select(svgRef.current), factor);
  };

  const resetZoom = () => {
    const z = zoomRef.current;
    if (!z || !svgRef.current) return;
    z.transform(d3.select(svgRef.current), d3.zoomIdentity);
  };

  return (
    <Card className="mx-4 mb-4" padded={false}>
      <div className="px-4 pt-3 pb-2 border-b border-border-dim">
        <div className="flex items-start justify-between gap-3">
          <div>
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
          {!empty && (
            <div className="flex items-center gap-1 shrink-0">
              <button type="button" onClick={() => zoomBy(1 / 1.3)} aria-label="Zoom out" className="p-1 text-text-muted hover:text-text-primary cursor-pointer">
                <ZoomOut size={14} />
              </button>
              <MonoText size="sm" muted>{zoomPct}%</MonoText>
              <button type="button" onClick={() => zoomBy(1.3)} aria-label="Zoom in" className="p-1 text-text-muted hover:text-text-primary cursor-pointer">
                <ZoomIn size={14} />
              </button>
              <button type="button" onClick={resetZoom} aria-label="Reset zoom" className="p-1 text-text-muted hover:text-text-primary cursor-pointer">
                <Maximize2 size={14} />
              </button>
            </div>
          )}
        </div>
      </div>
      {empty ? (
        <div className="flex items-center justify-center" style={{ height: HEIGHT }}>
          <MonoText size="sm" muted>No evidence yet. Run the decision to populate.</MonoText>
        </div>
      ) : (
        <svg ref={svgRef} className="w-full cursor-grab active:cursor-grabbing" style={{ height: HEIGHT }} />
      )}
    </Card>
  );
}
