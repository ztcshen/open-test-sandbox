import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { fetchJSON } from "./api.js";
import { TopologyDiagram, mergeTraceTopologies, parseTopology, topologyItems } from "./topologyView.jsx";

function queryParam(name) {
  return new URLSearchParams(window.location.search).get(name) || "";
}

function stepAnchor(stepID) {
  return `workflow-step-${encodeURIComponent(stepID || "unknown")}`;
}

function SummaryButton({ label, value, onClick }) {
  return (
    <button className="trace-topology-summary-action" type="button" onClick={onClick}>
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </button>
  );
}

function Chip({ children }) {
  return <span className="trace-topology-chip">{children}</span>;
}

function Empty({ children }) {
  return <div className="empty-note">{children}</div>;
}

function DiagramPanel({ topology, visible, total }) {
  return (
    <section className="trace-topology-panel">
      <div className="section-head"><div><h2>Topology graph</h2><p>{`${visible}/${total} persisted step traces`}</p></div></div>
      <TopologyDiagram topology={topology} markerPrefix="trace-topology" emptyLabel="没有匹配的 topology 可绘制。" />
    </section>
  );
}

function EdgeItem({ item, kind }) {
  return (
    <article className={`trace-topology-edge ${kind}`}>
      <div>
        <strong>{`${item.source || "-"} -> ${item.target || "-"}`}</strong>
        <span className="agent-status">{kind}</span>
      </div>
      <p>{[item.stepId, item.caseId, `${item.sourceComponent || item.component || "-"} -> ${item.targetComponent || item.endpoint || "-"}`].filter(Boolean).join(" · ")}</p>
      <code>{`${item.requestId || "-"} · ${item.traceId || "-"}`}</code>
      {item.workflowRunId && (item.stepId || item.caseId) ? (
        <div className="workflow-run-step-service-links trace-topology-step-link">
          <a href={`/workflow-run.html?id=${encodeURIComponent(item.workflowRunId)}#${stepAnchor(item.stepId || item.caseId)}`}>查看 step</a>
        </div>
      ) : null}
    </article>
  );
}

function filterRows(rows, query, status) {
  const normalized = query.trim().toLowerCase();
  const exact = normalized && rows.some((row) => String(row.stepId || row.caseId || "").toLowerCase() === normalized);
  return rows.filter((row) => {
    const parsed = parseTopology(row);
    const statusOK = !status || row.status === status;
    if (exact) return statusOK && String(row.stepId || row.caseId || "").toLowerCase() === normalized;
    const haystack = [
      row.stepId,
      row.caseId,
      row.requestId,
      row.traceId,
      row.status,
      ...(parsed.observedNodes || []),
      ...(parsed.confirmedEdges || []).flatMap((edge) => [edge.source, edge.target, edge.sourceComponent, edge.targetComponent]),
      ...(parsed.externalExits || []).flatMap((exit) => [exit.source, exit.target, exit.endpoint]),
      ...(parsed.unresolvedExits || []).flatMap((exit) => [exit.source, exit.target, exit.endpoint]),
    ].filter(Boolean).join(" ").toLowerCase();
    return statusOK && (!normalized || haystack.includes(normalized));
  });
}

function filterExits(exits, query, kind) {
  const normalized = query.trim().toLowerCase();
  const exact = normalized && exits.some((exit) => String(exit.stepId || exit.caseId || "").toLowerCase() === normalized);
  return exits.filter((exit) => {
    const kindOK = !kind || exit.kind === kind;
    if (exact) return kindOK && String(exit.stepId || exit.caseId || "").toLowerCase() === normalized;
    const haystack = [exit.stepId, exit.caseId, exit.source, exit.target, exit.sourceComponent, exit.targetComponent, exit.endpoint, exit.requestId, exit.traceId]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return kindOK && (!normalized || haystack.includes(normalized));
  });
}

function Matrix({ rows, total }) {
  return (
    <section className="trace-topology-panel">
      <div className="section-head"><div><h2>Step matrix</h2><p>{rows.length ? `${rows.length}/${total} persisted step traces` : `0/${total} persisted step traces`}</p></div></div>
      <div className="trace-topology-matrix">
        {rows.length ? rows.map((row) => {
          const parsed = parseTopology(row);
          return (
            <article className={`trace-topology-step ${row.status === "complete" ? "complete" : "partial"}`} key={`${row.workflowRunId}-${row.stepId || row.caseId}`}>
              <div>
                <strong>{row.stepId || row.caseId || "trace"}</strong>
                <span className={`status-pill ${row.status === "complete" ? "passed" : "failed"}`}>{row.status || "unknown"}</span>
              </div>
              <p>{[row.caseId, row.requestId || "-", row.traceId || "-", `spans ${parsed.spanCount || 0}`].filter(Boolean).join(" · ")}</p>
              <div className="trace-topology-chip-row">
                <Chip>{`${(parsed.confirmedEdges || []).length} edges`}</Chip>
                <Chip>{`${(parsed.externalExits || []).length} external`}</Chip>
                <Chip>{`${(parsed.unresolvedExits || []).length} unresolved`}</Chip>
                {(parsed.observedNodes || []).slice(0, 6).map((node) => <Chip key={node}>{node}</Chip>)}
              </div>
            </article>
          );
        }) : <Empty>{total ? "没有匹配的 topology。" : "此 Workflow run 暂无持久化 topology。"}</Empty>}
      </div>
    </section>
  );
}

function Edges({ rows }) {
  const edges = topologyItems(rows, "confirmedEdges");
  return (
    <section className="trace-topology-panel">
      <div className="section-head"><div><h2>Confirmed edges</h2><p>{edges.length ? `${edges.length} confirmed edges` : "0 confirmed edges"}</p></div></div>
      <div className="trace-topology-list">
        {edges.length ? edges.map((edge, index) => <EdgeItem item={edge} kind="confirmed" key={`${edge.stepId}-${edge.source}-${edge.target}-${index}`} />) : <Empty>没有返回可确认调用边。</Empty>}
      </div>
    </section>
  );
}

function Exits({ rows, query, exitKind }) {
  const external = topologyItems(rows, "externalExits").map((item) => ({ ...item, kind: "external" }));
  const unresolved = topologyItems(rows, "unresolvedExits").map((item) => ({ ...item, kind: "unresolved" }));
  const exits = filterExits([...unresolved, ...external], query, exitKind);
  return (
    <section className="trace-topology-panel">
      <div className="section-head"><div><h2>External and unresolved exits</h2><p>{`${exits.length} visible · ${external.length} external · ${unresolved.length} unresolved`}</p></div></div>
      <div className="trace-topology-list">
        {exits.length ? exits.map((exit, index) => <EdgeItem item={exit} kind={exit.kind} key={`${exit.stepId}-${exit.kind}-${exit.source}-${exit.target}-${index}`} />) : <Empty>此 run 没有 external 或 unresolved exit。</Empty>}
      </div>
    </section>
  );
}

function TraceTopologyApp() {
  const [payload, setPayload] = useState(null);
  const [message, setMessage] = useState("loading");
  const [query, setQuery] = useState(queryParam("traceFilter"));
  const [status, setStatus] = useState(queryParam("status"));
  const [exitKind, setExitKind] = useState(queryParam("exitKind"));
  const workflowRunID = queryParam("workflowRunId");

  async function refresh() {
    if (!workflowRunID) {
      setMessage("failed");
      setPayload({ error: "workflowRunId is required", traceTopologies: [], run: {} });
      return;
    }
    setMessage("refreshing...");
    try {
      setPayload(await fetchJSON(`/api/workflow-runs/${encodeURIComponent(workflowRunID)}`));
      setMessage("ready");
    } catch (error) {
      setPayload({ error: error.message, traceTopologies: [], run: {} });
      setMessage("failed");
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const rows = payload?.traceTopologies || [];
  const visibleRows = useMemo(() => filterRows(rows, query, status), [rows, query, status]);
  const visibleTopology = useMemo(() => mergeTraceTopologies(visibleRows), [visibleRows]);
  const run = payload?.run || {};
  const confirmed = topologyItems(rows, "confirmedEdges");
  const external = topologyItems(rows, "externalExits");
  const unresolved = topologyItems(rows, "unresolvedExits");
  const nodes = new Set(rows.flatMap((row) => parseTopology(row).observedNodes || []));
  const complete = rows.filter((row) => row.status === "complete").length;
  const partial = rows.filter((row) => row.status !== "complete").length;

  return (
    <main className="app trace-topology-page" data-template-id="TPL-SKYWALKING-TOPOLOGY-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-SKYWALKING-TOPOLOGY-V1</div>
      <section className="topbar">
        <div>
          <h1>Trace topology</h1>
          <p>{payload?.error ? "未找到 topology run" : `${run.workflowId || "-"} · #${run.id || "-"}`}</p>
        </div>
        <div className="actions">
          <span className="workflow-step-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">控制台</a>
          <a className="button-link" href={run.id ? `/workflow-run.html?id=${encodeURIComponent(run.id)}` : "/workflow-run.html"}>Workflow run</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
        </div>
      </section>
      <section className="trace-topology-summary" aria-label="Trace topology summary">
        <SummaryButton label="status" value={payload?.error ? "failed" : run.status || "-"} onClick={() => {}} />
        <SummaryButton label="records" value={String(rows.length)} onClick={() => {}} />
        <SummaryButton label="confirmed" value={String(confirmed.length)} onClick={() => {}} />
        <SummaryButton label="external" value={String(external.length)} onClick={() => setExitKind("external")} />
        <SummaryButton label="unresolved" value={String(unresolved.length)} onClick={() => setExitKind("unresolved")} />
        <SummaryButton label="nodes" value={String(nodes.size)} onClick={() => {}} />
      </section>
      <section className="trace-topology-controls" aria-label="Trace topology filters">
        <label className="workflow-filter">
          <span>筛选</span>
          <input type="search" placeholder="step / node / request / trace" spellCheck="false" title={`${visibleRows.length}/${rows.length} visible · complete ${complete} · partial ${partial}`} value={query} onChange={(event) => setQuery(event.target.value)} />
        </label>
        <select title="按 step topology 状态过滤" value={status} onChange={(event) => setStatus(event.target.value)}>
          <option value="">All status</option>
          <option value="complete">Complete</option>
          <option value="partial">Partial</option>
        </select>
        <select title="按 exit 类型过滤" value={exitKind} onChange={(event) => setExitKind(event.target.value)}>
          <option value="">All exits</option>
          <option value="unresolved">Unresolved</option>
          <option value="external">External</option>
        </select>
      </section>
      <section className="trace-topology-shell">
        <DiagramPanel topology={visibleTopology} visible={visibleRows.length} total={rows.length} />
        <Matrix rows={visibleRows} total={rows.length} />
        <Edges rows={visibleRows} />
        <Exits rows={visibleRows} query={query} exitKind={exitKind} />
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-trace-topology-root")).render(<TraceTopologyApp />);
