export function parseTopology(row) {
  if (!row?.topologyJson) return {};
  if (typeof row.topologyJson === "object") return row.topologyJson;
  try {
    return JSON.parse(row.topologyJson);
  } catch {
    return {};
  }
}

export function topologyItems(rows, key) {
  return rows.flatMap((row) => {
    const parsed = parseTopology(row);
    return (parsed[key] || []).map((item) => ({
      ...item,
      stepId: row.stepId,
      caseId: row.caseId,
      workflowRunId: row.workflowRunId,
      requestId: row.requestId,
      traceId: row.traceId,
      status: row.status,
    }));
  });
}

export function mergeTraceTopologies(rows = []) {
  const nodes = new Set();
  const merged = {
    status: "unavailable",
    observedNodes: [],
    confirmedEdges: [],
    externalExits: [],
    unresolvedExits: [],
  };
  let hasPartial = false;
  let hasComplete = false;

  rows.forEach((row) => {
    const parsed = parseTopology(row);
    (parsed.observedNodes || []).forEach((node) => {
      if (node) nodes.add(node);
    });
    ["confirmedEdges", "externalExits", "unresolvedExits"].forEach((key) => {
      (parsed[key] || []).forEach((item) => {
        if (item.source) nodes.add(item.source);
        if (item.target) nodes.add(item.target);
        merged[key].push({
          ...item,
          stepId: item.stepId || row.stepId,
          caseId: item.caseId || row.caseId,
          workflowRunId: item.workflowRunId || row.workflowRunId,
          requestId: item.requestId || row.requestId,
          traceId: item.traceId || row.traceId,
        });
      });
    });
    if (row.status === "complete" || parsed.status === "complete") hasComplete = true;
    if ((row.status && row.status !== "complete") || (parsed.status && parsed.status !== "complete")) hasPartial = true;
  });

  merged.status = hasPartial ? "partial" : hasComplete ? "complete" : "unavailable";
  merged.observedNodes = [...nodes].sort((left, right) => left.localeCompare(right));
  return merged;
}

export function topologyEdges(topology = {}) {
  return [
    ...(topology.confirmedEdges || []).map((edge) => ({ ...edge, kind: "confirmed" })),
    ...(topology.externalExits || []).map((edge) => ({ ...edge, kind: "external" })),
    ...(topology.unresolvedExits || []).map((edge) => ({ ...edge, kind: "unresolved" })),
  ];
}

export function topologyNodes(topology = {}, edges = []) {
  const nodeSet = new Set(topology.observedNodes || []);
  edges.forEach((edge) => {
    if (edge.source) nodeSet.add(edge.source);
    if (edge.target) nodeSet.add(edge.target);
  });
  return [...nodeSet].filter(Boolean);
}

export function topologyRanks(nodes, edges) {
  const rankMap = new Map(nodes.map((node) => [node, 0]));
  for (let pass = 0; pass < nodes.length; pass += 1) {
    let changed = false;
    edges.forEach((edge) => {
      if (!edge.source || !edge.target || !rankMap.has(edge.source) || !rankMap.has(edge.target)) return;
      const nextRank = (rankMap.get(edge.source) || 0) + 1;
      if (nextRank > (rankMap.get(edge.target) || 0)) {
        rankMap.set(edge.target, nextRank);
        changed = true;
      }
    });
    if (!changed) break;
  }
  return rankMap;
}

export function shortTopologyLabel(value, maxLength = 24) {
  const out = String(value || "-");
  return out.length > maxLength ? `${out.slice(0, maxLength - 1)}...` : out;
}

export function TopologyDiagram({ topology, markerPrefix = "topology", emptyLabel = "返回了记录，但没有可绘制节点。" }) {
  const edges = topologyEdges(topology);
  const nodes = topologyNodes(topology, edges);
  if (!nodes.length) return <div className="empty-note">{emptyLabel}</div>;
  const rankMap = topologyRanks(nodes, edges);
  const byRank = new Map();
  nodes.forEach((node) => {
    const rank = rankMap.get(node) || 0;
    if (!byRank.has(rank)) byRank.set(rank, []);
    byRank.get(rank).push(node);
  });
  byRank.forEach((rankNodes) => rankNodes.sort((left, right) => left.localeCompare(right)));
  const nodeWidth = 148;
  const nodeHeight = 46;
  const xGap = 205;
  const yGap = 82;
  const marginX = 42;
  const marginY = 36;
  const maxRank = Math.max(0, ...byRank.keys());
  const maxRows = Math.max(1, ...[...byRank.values()].map((rankNodes) => rankNodes.length));
  const width = Math.max(760, marginX * 2 + nodeWidth + maxRank * xGap);
  const height = Math.max(170, marginY * 2 + nodeHeight + (maxRows - 1) * yGap);
  const positions = new Map();
  byRank.forEach((rankNodes, rank) => {
    const columnHeight = (rankNodes.length - 1) * yGap;
    const startY = (height - columnHeight - nodeHeight) / 2;
    rankNodes.forEach((node, index) => positions.set(node, { x: marginX + rank * xGap, y: startY + index * yGap }));
  });
  return (
    <div className="workflow-step-topology-diagram">
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Service topology graph">
        <defs>
          {["confirmed", "external", "unresolved"].map((kind) => (
            <marker id={`${markerPrefix}-arrow-${kind}`} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse" key={kind}>
              <path d="M 0 0 L 10 5 L 0 10 z" className={`workflow-step-topology-arrow ${kind}`} />
            </marker>
          ))}
        </defs>
        {edges.map((edge, index) => {
          const source = positions.get(edge.source);
          const target = positions.get(edge.target);
          if (!source || !target) return null;
          const startX = source.x + nodeWidth;
          const startY = source.y + nodeHeight / 2;
          const endX = target.x;
          const endY = target.y + nodeHeight / 2;
          const control = Math.max(44, Math.abs(endX - startX) / 2);
          return (
            <g key={`${edge.source}-${edge.target}-${index}`}>
              <path d={`M ${startX} ${startY} C ${startX + control} ${startY}, ${endX - control} ${endY}, ${endX} ${endY}`} className={`workflow-step-topology-path ${edge.kind}`} markerEnd={`url(#${markerPrefix}-arrow-${edge.kind})`} />
              <text x={(startX + endX) / 2} y={(startY + endY) / 2 - 8} className={`workflow-step-topology-path-label ${edge.kind}`} textAnchor="middle">{edge.component || edge.sourceComponent || edge.kind}</text>
            </g>
          );
        })}
        {[...positions.entries()].map(([node, position]) => (
          <g className="workflow-step-topology-svg-node" key={node}>
            <rect x={position.x} y={position.y} width={nodeWidth} height={nodeHeight} rx="8" />
            <text x={position.x + 12} y={position.y + 20} className="workflow-step-topology-svg-node-title">{shortTopologyLabel(node, 22)}</text>
            <text x={position.x + 12} y={position.y + 36} className="workflow-step-topology-svg-node-meta">{`rank ${rankMap.get(node) || 0}`}</text>
          </g>
        ))}
      </svg>
    </div>
  );
}
