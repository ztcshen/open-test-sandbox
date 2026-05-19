import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { fetchJSON } from "./api.js";
import { buildEvidenceArtifacts, buildEvidenceNavigation, buildEvidenceReproduction, buildEvidenceTimeline } from "./evidenceTimelineModel.mjs";
import { TopologyDiagram, topologyEdges } from "./topologyView.jsx";
import { isSkyWalkingTopology, unavailableSkyWalkingTopology } from "./workflowStepModel.mjs";

const STORAGE_PREFIX = "open-test-sandbox-evidence:";

function query() {
  const params = new URLSearchParams(window.location.search);
  return {
    key: params.get("key") || "",
    caseRun: params.get("caseRun") || params.get("runId") || "",
    caseId: params.get("caseId") || "",
    stepId: params.get("stepId") || params.get("step") || "",
    workflowId: params.get("workflow") || params.get("workflowId") || "",
  };
}

function emptyFixtureEvidence() {
  return { status: "empty", applyRuns: [], summary: { applyCount: 0, restoreCount: 0, failedCount: 0 } };
}

function prettyJSON(value) {
  if (value === undefined || value === null || value === "") return "-";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

function normalizeCaseEvidence(payload) {
  const evidence = payload.evidence || {};
  const summary = evidence.summary || {};
  const trace = evidence.trace || {};
  const request = evidence.request || {};
  const response = evidence.response || {};
  const systems = Array.isArray(evidence.logs) ? evidence.logs : Array.isArray(trace.systems) ? trace.systems : [];
  const continuity = trace.trace_continuity || trace.traceContinuity || {};
  const continuityStatus = continuity.status || (continuity.ok === true ? "passed" : continuity.ok === false ? "failed" : "unknown");
  return {
    step: {
      title: summary.case_id || "Case run evidence",
      goal: summary.operation || "Case run evidence",
      stageTitle: "API Case",
      caseId: summary.case_id || "-",
      path: (trace.required_systems || trace.requiredSystems || []).join(" -> "),
      correlators: trace.correlators || [],
      systems: systems.map((system) => ({
        id: system.id,
        name: system.name || system.id,
        found: Boolean(system.found),
        coreLogs: system.coreLogs || system.lines || [],
        error: system.message || system.error || "",
      })),
      traceContinuity: {
        status: continuityStatus,
        reason: continuity.reason || "",
        requestId: summary.request_id || trace.requestId || summary.trace_id || "",
        matchedSystems: continuity.matched_systems || continuity.matchedSystems || [],
        missingSystems: continuity.missing_systems || continuity.missingSystems || [],
      },
      meta: `${request.method || request.sdk_operation || request.sdkOperation || "request"} / ${response.http_code || "-"}`,
      topology: evidence.topology || {},
    },
    caseDiagnostics: {
      summary,
      request,
      response,
      assertions: evidence.assertions || {},
      services: Array.isArray(evidence.services) ? evidence.services : [],
      mysql: evidence.mysql || {},
      fixture: evidence.fixture || emptyFixtureEvidence(),
      topology: evidence.topology || {},
    },
  };
}

async function loadPayload() {
  const { key, caseRun, caseId, stepId } = query();
  if (caseRun) {
    const params = new URLSearchParams({ runId: caseRun });
    if (caseId) params.set("caseId", caseId);
    if (stepId) params.set("stepId", stepId);
    return normalizeCaseEvidence(await fetchJSON(`/api/case/evidence?${params.toString()}`));
  }
  if (!key.startsWith(STORAGE_PREFIX)) return null;
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function normalizeStep(step) {
  const trace = step.trace || {};
  const traceCorrelators = Array.isArray(trace.correlators) ? trace.correlators : [];
  const stepCorrelators = Array.isArray(step.correlators) ? step.correlators : [];
  const traceSystems = Array.isArray(trace.systems) ? trace.systems : [];
  const stepSystems = Array.isArray(step.systems) ? step.systems : [];
  const visited = Array.isArray(trace.visited) ? trace.visited : [];
  return {
    ...step,
    path: step.path || (visited.length ? visited.join(" -> ") : ""),
    requestId: step.requestId || trace.requestId || "",
    traceContinuity: step.traceContinuity || trace.traceContinuity || null,
    correlators: stepCorrelators.length ? stepCorrelators : traceCorrelators,
    systems: stepSystems.length ? stepSystems : traceSystems,
  };
}

function deriveTraceContinuity(step) {
  if (step.traceContinuity) return step.traceContinuity;
  const requestID = step.requestId || (step.correlators || [])[0] || "";
  if (!requestID) return null;
  const matchedSystems = [];
  const missingSystems = [];
  (step.systems || []).filter((system) => system.found).forEach((system) => {
    const matched = (system.coreLogs || []).some((line) => line.includes(requestID));
    if (matched) matchedSystems.push(system.id);
    else missingSystems.push(system.id);
  });
  return {
    status: missingSystems.length ? "partial" : matchedSystems.length ? "passed" : "failed",
    reason: missingSystems.length ? `缺失系统: ${missingSystems.join(", ")}` : "当前已加载日志都包含 trace id",
    requestId: requestID,
    matchedSystems,
    missingSystems,
  };
}

function summarizeLogLine(line) {
  let summary = String(line || "").trim();
  summary = summary.replace(/^\[?\d{4}-\d{2}-\d{2}[^\]]*\]?\s*/, "");
  summary = summary.replace(/^(\[[^\]]+\]\s*)+/, "");
  summary = summary.replace(/\s+/g, " ").trim();
  if (!summary) return "日志详情";
  return summary.length > 140 ? `${summary.slice(0, 140)}...` : summary;
}

function extractCodeHints(systems = []) {
  const hints = [];
  const seen = new Set();
  const javaRefPattern = /\[([A-Za-z0-9_$.]+\.java:[A-Za-z0-9_$<>]+:\d+)\]/g;
  const classLinePattern = /\]\s+\[[A-Z]+\s+\]\s+([A-Za-z0-9_$.]+)\s+(\d+)\s+--/;
  systems.forEach((system) => {
    (system.coreLogs || []).forEach((line) => {
      let match;
      while ((match = javaRefPattern.exec(line)) !== null) {
        const ref = match[1];
        const key = `${system.id}:${ref}`;
        if (!seen.has(key)) {
          seen.add(key);
          hints.push({ systemId: system.id, systemName: system.name, ref, sample: summarizeLogLine(line) });
        }
      }
      const classMatch = line.match(classLinePattern);
      if (!classMatch) return;
      const ref = `${classMatch[1]}:${classMatch[2]}`;
      const key = `${system.id}:${ref}`;
      if (!seen.has(key)) {
        seen.add(key);
        hints.push({ systemId: system.id, systemName: system.name, ref, sample: summarizeLogLine(line) });
      }
    });
  });
  return hints.slice(0, 12);
}

function Card({ title, meta, className = "", children }) {
  return (
    <section className={`viewer-card ${className}`.trim()}>
      <div className="viewer-card-head">
        <h2>{title}</h2>
        <span>{meta}</span>
      </div>
      {children}
    </section>
  );
}

function Diagnostic({ label, value, detail }) {
  return (
    <article className="viewer-diagnostic-item">
      <span>{label}</span>
      <strong>{value || "-"}</strong>
      <p>{detail || "-"}</p>
    </article>
  );
}

function SignalCard({ step, codeHints }) {
  const continuity = deriveTraceContinuity(step) || {};
  const signals = [
    ["TRACE CONTINUITY", continuity.status || "unknown", continuity.reason || "没有 continuity 结论"],
    ["REQUEST ID", step.requestId || continuity.requestId || "-", (step.correlators || []).join(" · ") || "没有关联字段"],
    ["MATCHED SYSTEMS", (continuity.matchedSystems || []).join(", ") || "-", (continuity.missingSystems || []).length ? `缺失: ${continuity.missingSystems.join(", ")}` : "当前匹配系统都带有 trace id"],
  ];
  return (
    <Card title="排障信号" meta="Trace / code focus" className="viewer-signal-card">
      <div className="viewer-signal-list">
        {signals.map(([label, value, detail]) => <Diagnostic label={label} value={value} detail={detail} key={label} />)}
      </div>
      <div className="viewer-code-hints">
        <h3>疑似代码入口</h3>
        {codeHints.length ? (
          <div className="viewer-code-hint-list">
            {codeHints.map((hint) => (
              <article className="viewer-code-hint" key={`${hint.systemId}-${hint.ref}`}>
                <strong>{hint.systemName || hint.systemId}</strong>
                <code>{hint.ref}</code>
                <p>{hint.sample}</p>
              </article>
            ))}
          </div>
        ) : <p className="viewer-code-hint-empty">当前日志里没有提取到稳定的类 / 方法位点。</p>}
      </div>
    </Card>
  );
}

function fixtureSummary(fixture = {}) {
  const applyRuns = Array.isArray(fixture.applyRuns) ? fixture.applyRuns : [];
  const dependencies = Array.isArray(fixture.dependencies) ? fixture.dependencies : [];
  const upstreamSteps = Array.isArray(fixture.upstreamSteps) ? fixture.upstreamSteps : [];
  const summary = fixture.summary || {};
  return {
    status: fixture.status || (applyRuns.length ? applyRuns[applyRuns.length - 1]?.status : "empty"),
    applyCount: Number(summary.applyCount || applyRuns.filter((run) => run.status === "applied").length || 0),
    restoreCount: Number(summary.restoreCount || applyRuns.filter((run) => run.status === "restored").length || 0),
    failedCount: Number(summary.failedCount || applyRuns.filter((run) => String(run.status || "").includes("failed")).length || 0),
    dependencyCount: Number(summary.dependencyCount || dependencies.length || 0),
    upstreamCount: Number(summary.upstreamCount || upstreamSteps.length || 0),
    applyRuns,
    dependencies,
    upstreamSteps,
  };
}

function FixtureEvidence({ fixture }) {
  const summary = fixtureSummary(fixture || emptyFixtureEvidence());
  const hasConfiguredPlan = summary.dependencies.length || summary.upstreamSteps.length;
  return (
    <Card title="前置证据" meta={`${summary.status || "empty"} · ${summary.applyRuns.length} runs`} className="viewer-fixture-evidence">
      {summary.applyRuns.length ? (
        <>
          <div className="viewer-diagnostic-grid">
            <Diagnostic label="PRECONDITION STATUS" value={summary.status} detail={`${summary.applyCount} apply · ${summary.restoreCount} restore · ${summary.failedCount} failed`} />
            <Diagnostic label="PRECONDITION INSTANCE" value={summary.applyRuns[0]?.fixtureInstanceId || "-"} detail="来自运行前自动选取的前置数据包" />
            <Diagnostic label="CLEANUP" value={summary.failedCount ? "needs attention" : "restored"} detail="执行后按运行前快照恢复现场" />
          </div>
          <div className="viewer-fixture-run-list">
            {summary.applyRuns.map((run, index) => (
              <article className="viewer-fixture-run" key={`${run.fixtureInstanceId || "fixture"}-${index}`}>
                <div className="viewer-card-head">
                  <h3>{run.status || "-"}</h3>
                  <span>{run.fixtureInstanceId || "-"}</span>
                </div>
                <pre className="viewer-pre">{prettyJSON({ appliedRows: run.appliedRows || {}, cleanupSql: Array.isArray(run.cleanupSql) ? run.cleanupSql : [], failureReason: run.failureReason || "" })}</pre>
              </article>
            ))}
          </div>
        </>
      ) : hasConfiguredPlan ? (
        <>
          <div className="viewer-diagnostic-grid">
            <Diagnostic label="PRECONDITION STATUS" value={summary.status} detail="Catalog 已声明前置数据依赖" />
            <Diagnostic label="DEPENDENCIES" value={String(summary.dependencyCount)} detail="运行前需要满足的数据包" />
            <Diagnostic label="UPSTREAM STEPS" value={String(summary.upstreamCount)} detail="当前 Case 之前的 Workflow 步骤" />
          </div>
          {summary.dependencies.length ? (
            <div className="viewer-fixture-run-list">
              {summary.dependencies.map((dependency, index) => (
                <article className="viewer-fixture-run" key={dependency.id || `${dependency.fixtureProfileId}-${index}`}>
                  <div className="viewer-card-head">
                    <h3>{dependency.profile?.name || dependency.fixtureProfileId || dependency.id}</h3>
                    <span>{dependency.required ? "required" : "optional"}</span>
                  </div>
                  <pre className="viewer-pre">{prettyJSON({
                    fixtureProfileId: dependency.fixtureProfileId,
                    sourceWorkflowId: dependency.profile?.sourceWorkflowId,
                    sourceUntilStep: dependency.profile?.sourceUntilStep,
                    mappings: dependency.mappings || [],
                    sourceSteps: dependency.profile?.sourceSteps || [],
                  })}</pre>
                </article>
              ))}
            </div>
          ) : null}
          {summary.upstreamSteps.length ? (
            <div className="workflow-step-topology-edges">
              {summary.upstreamSteps.map((step) => (
                <article className="workflow-step-topology-edge confirmed" key={`${step.workflowId}-${step.stepId}`}>
                  <strong>{step.stepId || "-"}</strong>
                  <span>{"->"}</span>
                  <strong>{step.nodeId || "-"}</strong>
                  <code>{step.caseId || "-"}</code>
                </article>
              ))}
            </div>
          ) : null}
        </>
      ) : <p className="viewer-code-hint-empty">当前 Case 不需要前置证据，或本次运行没有应用前置数据。</p>}
    </Card>
  );
}

function TopologyCard({ topology }) {
  const trustedTopology = isSkyWalkingTopology(topology) ? topology : unavailableSkyWalkingTopology();
  if (!trustedTopology.status && !trustedTopology.traceId && !trustedTopology.requestId) return null;
  const edges = topologyEdges(trustedTopology);
  return (
    <Card title="Topology" meta={`${trustedTopology.status || "-"} · ${trustedTopology.requestId || "-"} · ${trustedTopology.traceId || "-"}`} className="viewer-case-topology">
      <TopologyDiagram topology={trustedTopology} markerPrefix="evidence" />
      <div className="workflow-step-topology-edges">
        {edges.length ? edges.map((edge, index) => (
          <article className={`workflow-step-topology-edge ${edge.kind}`} key={`${edge.source}-${edge.target}-${index}`}>
            <strong>{edge.source || "-"}</strong>
            <span>{"->"}</span>
            <strong>{edge.target || "-"}</strong>
            <code>{`${edge.kind}${edge.endpoint || edge.targetComponent || edge.component ? ` · ${edge.endpoint || edge.targetComponent || edge.component}` : ""}`}</code>
          </article>
        )) : <div className="empty-note">没有确认调用边；保留当前 trace 状态。</div>}
      </div>
      {trustedTopology.textTopology ? <pre className="viewer-pre">{trustedTopology.textTopology}</pre> : null}
    </Card>
  );
}

function failedAssertionKeys(assertions = {}) {
  return Object.entries(assertions)
    .filter(([key, value]) => (key.endsWith("_ok") || key === "passed") && value === false)
    .map(([key]) => key);
}

function CaseDiagnostics({ diagnostics }) {
  if (!diagnostics) return null;
  const { summary = {}, request = {}, response = {}, assertions = {}, services = [], mysql = {}, fixture = emptyFixtureEvidence() } = diagnostics;
  const failed = failedAssertionKeys(assertions);
  const okServices = services.filter((service) => service.ok).length;
  const queryCount = Array.isArray(mysql.queries) ? mysql.queries.length : 0;
  const sqlRows = Array.isArray(mysql.queries) ? mysql.queries.reduce((total, item) => total + Number(item.row_count || item.rowCount || 0), 0) : 0;
  const expectedCodes = assertions.expected_http_codes || summary.expected_http_codes || [];
  const fixtureStats = fixtureSummary(fixture);
  return (
    <Card title="API Case Diagnostics" meta="HTTP / ASSERTIONS / SQL" className="viewer-case-diagnostics">
      <div className="viewer-diagnostic-grid">
        <Diagnostic label="HTTP STATUS" value={String(response.http_code || assertions.actual_http_code || summary.actual_http_code || "-")} detail={`expected ${expectedCodes.join(", ") || "-"} · request ${response.request_id || "-"}`} />
        <Diagnostic label="FAILURE KIND" value={summary.failure_kind || assertions.failure_kind || "none"} detail={summary.failure_reason || assertions.failure_reason || "no failure reason"} />
        <Diagnostic label="ASSERTIONS" value={failed.length ? `${failed.length} failed` : "passed"} detail={failed.join(", ") || "all tracked assertions passed"} />
        <Diagnostic label="SQL" value={mysql.ok === false ? "failed" : "ok"} detail={`${queryCount} queries · ${sqlRows} rows`} />
        <Diagnostic label="PRECONDITION" value={fixtureStats.status || "empty"} detail={`${fixtureStats.applyCount} apply · ${fixtureStats.restoreCount} restore · ${fixtureStats.dependencyCount} dependencies`} />
        <Diagnostic label="SERVICES" value={`${okServices}/${services.length || 0} ok`} detail={services.map((service) => `${service.id}:${service.health || service.state || "-"}`).join(" · ") || "no service snapshot"} />
        <Diagnostic label="REQUEST" value={request.sdk_operation || request.sdkOperation || request.method || "-"} detail={summary.evidence_path || "runtime case bundle"} />
      </div>
      <h3 className="viewer-raw-title">RAW CASE BUNDLE</h3>
      <pre className="viewer-pre viewer-raw-case-bundle">{prettyJSON(diagnostics)}</pre>
    </Card>
  );
}

function LogCard({ system }) {
  const logs = system.coreLogs?.length ? system.coreLogs.join("\n\n") : system.error || "未匹配到核心日志";
  return (
    <Card title={system.name} meta={`${system.coreLogs?.length || 0} 条核心日志`}>
      <pre className="viewer-pre">{logs}</pre>
    </Card>
  );
}

function TimelineTone({ tone }) {
  return <code className={`viewer-timeline-tone ${tone || "neutral"}`}>{tone || "neutral"}</code>;
}

function EvidenceTimelineCard({ timeline, query, type, onQuery, onType, onSelect }) {
  if (!timeline.items.length) return null;
  const selected = timeline.selectedItem;
  return (
    <Card title="Evidence Timeline" meta={`${timeline.summary.visible}/${timeline.summary.total} sections`} className="viewer-timeline-card">
      <div className="viewer-timeline-toolbar">
        <div className="viewer-timeline-facets">
          <button className={`detail-tab ${type ? "" : "active"}`.trim()} type="button" onClick={() => onType("")}>
            All
          </button>
          {timeline.facets.map((facet) => (
            <button className={`detail-tab ${type === facet.key ? "active" : ""}`.trim()} type="button" key={facet.key} onClick={() => onType(facet.key)}>
              {`${facet.label} ${facet.count}`}
            </button>
          ))}
        </div>
        <label className="workflow-filter viewer-timeline-search">
          <span>Search</span>
          <input type="search" value={query} placeholder="request / log / status" spellCheck="false" onChange={(event) => onQuery(event.target.value)} />
        </label>
      </div>
      <div className="viewer-timeline-workbench">
        <div className="viewer-timeline-list" aria-label="Evidence timeline sections">
          {timeline.visibleItems.length ? timeline.visibleItems.map((item) => (
            <button className={`viewer-timeline-item ${selected?.id === item.id ? "selected" : ""}`.trim()} type="button" key={item.id} onClick={() => onSelect(item.id)}>
              <span>{item.type}</span>
              <strong>{item.title}</strong>
              <p>{item.detail || item.status || "-"}</p>
              <div>
                <TimelineTone tone={item.tone} />
                <code>{item.status || item.meta || "-"}</code>
              </div>
            </button>
          )) : <p className="viewer-code-hint-empty">当前筛选下没有 Evidence section。</p>}
        </div>
        <article className="viewer-timeline-detail">
          {selected ? (
            <>
              <div className="viewer-card-head">
                <h3>{selected.title}</h3>
                <span>{selected.meta || selected.status || selected.type}</span>
              </div>
              <pre className="viewer-pre">{selected.preview || prettyJSON(selected.payload)}</pre>
            </>
          ) : <p className="viewer-code-hint-empty">没有可展示的 Evidence section。</p>}
        </article>
      </div>
    </Card>
  );
}

function EvidenceArtifactsCard({ artifacts }) {
  if (!artifacts.length) return null;
  return (
    <Card title="Evidence Artifacts" meta={`${artifacts.length} linked artifacts`} className="viewer-artifacts-card">
      <div className="viewer-artifact-list">
        {artifacts.map((artifact) => {
          const body = (
            <>
              <span>{artifact.kind || "artifact"}</span>
              <strong>{artifact.label || artifact.path}</strong>
              <code>{artifact.path}</code>
            </>
          );
          return artifact.href ? (
            <a className="viewer-artifact-item" href={artifact.href} key={artifact.id}>
              {body}
            </a>
          ) : (
            <article className="viewer-artifact-item" key={artifact.id}>
              {body}
            </article>
          );
        })}
      </div>
    </Card>
  );
}

function ReproductionCommandCard({ reproduction }) {
  if (!reproduction?.available) return null;
  return (
    <Card title="Reproduction Command" meta={`${reproduction.method} · ${reproduction.status}`} className="viewer-reproduction-card">
      <div className="viewer-diagnostic-grid">
        <Diagnostic label="TARGET" value={reproduction.url} detail={reproduction.failure || "request evidence captured"} />
      </div>
      <pre className="viewer-pre">{reproduction.command}</pre>
    </Card>
  );
}

function EmptyViewer({ subtitle, navigation = buildEvidenceNavigation({}) }) {
  return (
    <main className="app viewer-app">
      <section className="viewer-topbar">
        <div>
          <p className="viewer-eyebrow">Evidence Viewer</p>
          <h1>日志不可用</h1>
          <p className="viewer-subtitle">{subtitle || "没有找到当前步骤的日志快照，请回到主页面重新打开。"}</p>
        </div>
        <div className="viewer-meta">
          <span className="detail-phase">阶段</span>
          <span className="viewer-case">-</span>
          <nav className="viewer-actions" aria-label="Evidence navigation">
            {navigation.workflowCaseSetHref ? <a className="button-link" href={navigation.workflowCaseSetHref}>Workflow case set</a> : null}
            <a className="button-link" href={navigation.caseRunsHref}>API Case Evidence</a>
            <a className="button-link" href="/">控制台</a>
          </nav>
        </div>
      </section>
      <section className="viewer-summary">
        {["经过系统", "关联字段", "请求 / 返回", "Trace Continuity", "代码位点"].map((label) => <article className="summary-card" key={label}><span>{label}</span><strong>-</strong></article>)}
      </section>
      <section className="viewer-grid"><section className="viewer-card"><pre className="viewer-pre">没有找到日志快照。</pre></section></section>
    </main>
  );
}

function EvidenceViewerApp() {
  const queryContext = useMemo(() => query(), []);
  const navigation = useMemo(() => buildEvidenceNavigation(queryContext), [queryContext]);
  const [payload, setPayload] = useState(null);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [timelineQuery, setTimelineQuery] = useState("");
  const [timelineType, setTimelineType] = useState("");
  const [selectedEvidenceId, setSelectedEvidenceId] = useState("");

  useEffect(() => {
    loadPayload()
      .then((next) => setPayload(next))
      .catch((loadError) => setError(loadError.message || "Evidence 加载失败。"))
      .finally(() => setLoaded(true));
  }, []);

  const step = useMemo(() => payload?.step ? normalizeStep(payload.step) : null, [payload]);
  const codeHints = useMemo(() => extractCodeHints(step?.systems || []), [step]);
  const continuity = step ? deriveTraceContinuity(step) || {} : {};
  const systems = (step?.systems || []).filter((system) => system.found);
  const timeline = useMemo(
    () => buildEvidenceTimeline(payload || {}, { query: timelineQuery, type: timelineType, selectedId: selectedEvidenceId }),
    [payload, timelineQuery, timelineType, selectedEvidenceId],
  );
  const artifacts = useMemo(() => buildEvidenceArtifacts(payload || {}), [payload]);
  const reproduction = useMemo(() => buildEvidenceReproduction(payload || {}), [payload]);
  if (!loaded) return <EmptyViewer subtitle="Evidence loading" navigation={navigation} />;
  if (error || !payload || !step) return <EmptyViewer subtitle={error} navigation={navigation} />;
  return (
    <main className="app viewer-app">
      <section className="viewer-topbar">
        <div>
          <p className="viewer-eyebrow">Evidence Viewer</p>
          <h1>{step.title || "日志查看页"}</h1>
          <p className="viewer-subtitle">{step.goal || "查看当前步骤的完整系统日志。"}</p>
        </div>
        <div className="viewer-meta">
          <span className="detail-phase">{step.stageTitle || "阶段"}</span>
          <span className="viewer-case">{step.caseId || "-"}</span>
          <nav className="viewer-actions" aria-label="Evidence navigation">
            {navigation.workflowCaseSetHref ? <a className="button-link" href={navigation.workflowCaseSetHref}>Workflow case set</a> : null}
            <a className="button-link" href={navigation.caseRunsHref}>API Case Evidence</a>
            <a className="button-link" href="/">控制台</a>
          </nav>
        </div>
      </section>
      <section className="viewer-summary">
        <article className="summary-card"><span>经过系统</span><strong>{step.path || "-"}</strong></article>
        <article className="summary-card"><span>关联字段</span><strong>{(step.correlators || []).join(" · ") || "-"}</strong></article>
        <article className="summary-card"><span>请求 / 返回</span><strong>{step.meta || "-"}</strong></article>
        <article className="summary-card"><span>Trace Continuity</span><strong>{continuity.status ? `${continuity.status} · ${(continuity.matchedSystems || []).length} systems` : "-"}</strong></article>
        <article className="summary-card"><span>代码位点</span><strong>{codeHints.length ? `${codeHints.length} 个定位提示` : "0 个定位提示"}</strong></article>
      </section>
      <section className="viewer-grid">
        <EvidenceTimelineCard
          timeline={timeline}
          query={timelineQuery}
          type={timelineType}
          onQuery={setTimelineQuery}
          onType={(nextType) => {
            setTimelineType(nextType);
            setSelectedEvidenceId("");
          }}
          onSelect={setSelectedEvidenceId}
        />
        <EvidenceArtifactsCard artifacts={artifacts} />
        <ReproductionCommandCard reproduction={reproduction} />
        <SignalCard step={step} codeHints={codeHints} />
        <FixtureEvidence fixture={payload.caseDiagnostics?.fixture || emptyFixtureEvidence()} />
        <TopologyCard topology={step.topology || payload.caseDiagnostics?.topology} />
        <CaseDiagnostics diagnostics={payload.caseDiagnostics} />
        {systems.length ? systems.map((system) => <LogCard system={system} key={system.id || system.name} />) : <section className="viewer-card"><pre className="viewer-pre">当前步骤没有采集到可展示的日志。</pre></section>}
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-evidence-viewer-root")).render(<EvidenceViewerApp />);
