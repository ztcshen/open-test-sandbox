import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Chip, fetchJSON, queryParam, selectedStep, selectedWorkflow, serviceName, workflowIdFromURL } from "./workflowPagesCommon.jsx";
import { TopologyDiagram, parseTopology, topologyEdges, topologyNodes } from "./topologyView.jsx";

function unique(values) {
  return [...new Set((values || []).filter(Boolean))];
}

function stepCopy(step, key, fallback) {
  return step?.presentation?.copy?.[key] || fallback;
}

function runtimeText(runtime) {
  if (!runtime) return "missing";
  return [runtime.state || "unknown", runtime.health || runtime.message || ""].filter(Boolean).join(" · ");
}

function formatMs(value) {
  const ms = Number(value);
  if (!Number.isFinite(ms) || ms < 0) return "-";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms % 1000 === 0 ? 0 : 1)} s`;
}

function stepHref(workflowID, stepID, runID = "") {
  const params = new URLSearchParams({ workflow: workflowID || "", step: stepID || "" });
  if (runID) params.set("runId", runID);
  return `/workflow-step.html?${params.toString()}`;
}

function ContextCard({ title, values, empty }) {
  const items = unique(values);
  return (
    <article className="workflow-step-context-card">
      <strong>{title}</strong>
      <div className="workflow-detail-chips">
        {items.length ? items.map((value) => <Chip key={value}>{value}</Chip>) : <Chip>{empty}</Chip>}
      </div>
    </article>
  );
}

function configValue(value) {
  if (value === undefined || value === null || value === "") return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function namedConfigItem(item, keys) {
  if (!item || typeof item !== "object") return configValue(item);
  const parts = keys.map((key) => configValue(item[key])).filter(Boolean);
  return parts.length ? parts.join(" · ") : configValue(item);
}

function ConfigCard({ title, values, empty }) {
  return <ContextCard title={title} values={(values || []).filter(Boolean)} empty={empty} />;
}

function StepTemplateConfig({ step }) {
  return (
    <section className="workflow-step-context">
      <div className="section-head">
        <div>
          <h2>{stepCopy(step, "stepConfigTitle", "Step 配置")}</h2>
          <p>{step?.id || "loading"}</p>
        </div>
      </div>
      <div className="workflow-step-context-grid">
        <ConfigCard title={stepCopy(step, "runConfigLabel", "Run")} values={[step?.executable ? "executable" : "", step?.required ? "required" : "optional"]} empty={stepCopy(step, "runConfigEmpty", "not executable")} />
        <ConfigCard title={stepCopy(step, "evidenceConfigLabel", "Evidence")} values={step?.evidenceKinds || []} empty={stepCopy(step, "evidenceConfigEmpty", "无 Evidence")} />
        <ConfigCard title={stepCopy(step, "targetsConfigLabel", "Targets")} values={step?.relatedMockTargets || []} empty={stepCopy(step, "targetsConfigEmpty", "无 Target")} />
        <ConfigCard title={stepCopy(step, "inputsConfigLabel", "Inputs")} values={(step?.inputs || []).map((item) => namedConfigItem(item, ["name", "source", "path"]))} empty={stepCopy(step, "inputsConfigEmpty", "无 Input")} />
        <ConfigCard title={stepCopy(step, "exportsConfigLabel", "Exports")} values={(step?.exports || []).map((item) => namedConfigItem(item, ["name", "from", "path"]))} empty={stepCopy(step, "exportsConfigEmpty", "无 Export")} />
      </div>
    </section>
  );
}

function StepContext({ workflow, step, services }) {
  const steps = workflow?.steps || [];
  const index = steps.findIndex((item) => item.id === step?.id);
  return (
    <section className="workflow-step-context">
      <div className="section-head">
        <div>
          <h2>{stepCopy(step, "contextTitle", "上下文摘要")}</h2>
          <p>{workflow && step ? `${Math.max(index, 0) + 1} / ${steps.length || 0} · ${workflow.displayName || workflow.id}` : "loading"}</p>
        </div>
      </div>
      <div className="workflow-step-context-grid">
        <ContextCard title={stepCopy(step, "currentServiceLabel", "当前服务")} values={[serviceName(services, step?.serviceId)]} empty={stepCopy(step, "currentServiceEmpty", "未声明服务")} />
        <ContextCard title={stepCopy(step, "workflowActionLabel", "Workflow action")} values={steps.map((item) => item.action)} empty={stepCopy(step, "workflowActionEmpty", "无 action")} />
        <ContextCard title={stepCopy(step, "workflowEvidenceLabel", "Workflow evidence")} values={steps.flatMap((item) => item.evidenceKinds || [])} empty={stepCopy(step, "workflowEvidenceEmpty", "无 Evidence")} />
        <ContextCard title={stepCopy(step, "workflowCasesLabel", "Workflow cases")} values={steps.map((item) => item.caseId)} empty={stepCopy(step, "workflowCasesEmpty", "无 case")} />
      </div>
    </section>
  );
}

function ServiceEvidence({ step, service, runtime }) {
  const rows = [
    ["service id", step?.serviceId || "-"],
    ["kind", service?.kind || "-"],
    ["runtime", runtimeText(runtime)],
    ["health", runtime?.health || "-"],
  ];
  return (
    <section className="workflow-step-service-evidence">
      <div className="section-head">
        <div>
          <h2>{stepCopy(step, "serviceEvidenceTitle", "服务证据")}</h2>
          <p>{service ? `${service.displayName || service.id} · ${service.role || "service"} · ${runtimeText(runtime)}` : `${step?.serviceId || "-"} · ${stepCopy(step, "serviceUnmodeledLabel", "未建模")}`}</p>
        </div>
        <div className="workflow-step-service-actions">
          {step?.serviceId ? <a className="button-link" href={`/environment-node.html?id=${encodeURIComponent(step.serviceId)}`}>{stepCopy(step, "environmentNodeLink", "环境节点详情")}</a> : null}
          <a className="button-link" href="/service-inventory.html">{stepCopy(step, "serviceInventoryLink", "服务清单")}</a>
        </div>
      </div>
      <dl className="workflow-step-service-meta">
        {rows.flatMap(([label, value]) => [
          <dt key={`${label}-term`}>{label}</dt>,
          <dd key={`${label}-value`}>{value || "-"}</dd>,
        ])}
      </dl>
    </section>
  );
}

function stepEvidenceHref(runID, step) {
  const params = new URLSearchParams({ caseRun: runID || "" });
  if (step?.caseId) params.set("caseId", step.caseId);
  if (step?.id) params.set("stepId", step.id);
  return `/evidence-viewer.html?${params.toString()}`;
}

function stepTopologyHref(runID, step) {
  const params = new URLSearchParams({ workflowRunId: runID || "" });
  if (step?.id) params.set("traceFilter", step.id);
  return `/trace-topology.html?${params.toString()}`;
}

function objectValue(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function firstEvidenceValue(...values) {
  return values.find((value) => {
    if (value === undefined || value === null) return false;
    if (typeof value === "object") return Object.keys(value).length > 0;
    return String(value).trim() !== "";
  });
}

function prettyEvidence(value) {
  if (value === undefined || value === null || value === "") return "{}";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function requestEvidence(stepResult) {
  const result = objectValue(stepResult?.result);
  return firstEvidenceValue(result.request, stepResult?.request, objectValue(stepResult?.details).request) || {};
}

function responseEvidence(stepResult) {
  const result = objectValue(stepResult?.result);
  return firstEvidenceValue(result.response, stepResult?.response, objectValue(stepResult?.details).response) || {};
}

function topologyFromStepRun(stepRun, stepResult) {
  if (stepResult?.traceTopology && Object.keys(stepResult.traceTopology).length) return stepResult.traceTopology;
  const rows = stepRun?.traceTopologies || [];
  const row = rows.find((item) => !stepResult?.stepId || item.stepId === stepResult.stepId) || rows[0];
  return parseTopology(row);
}

function logSystems(stepResult) {
  const systems = stepResult?.trace?.systems;
  return Array.isArray(systems) ? systems : [];
}

function logLines(system) {
  const lines = system?.coreLogs || system?.logs || system?.lines || [];
  if (Array.isArray(lines)) return lines;
  if (typeof lines === "string") return [lines];
  return [];
}

function logSystemName(system) {
  return system?.name || system?.id || system?.serviceId || "service";
}

function stepRunNeedsRefresh(stepRun) {
  const step = stepRun?.summary?.steps?.[0];
  if (!step) return false;
  const systems = logSystems(step);
  const pendingLogs = systems.some((system) => system.pending);
  const topology = topologyFromStepRun(stepRun, step);
  return pendingLogs || (!topology?.traceId && !(stepRun?.traceTopologies || []).length);
}

function StepEvidenceTemplate({ stepRun, stepResult, step }) {
  const request = requestEvidence(stepResult);
  const response = responseEvidence(stepResult);
  const topology = topologyFromStepRun(stepRun, stepResult);
  const edges = topologyEdges(topology);
  const nodes = topologyNodes(topology, edges);
  const systems = logSystems(stepResult);
  const requestID = topology.requestId || objectValue(response.headers)["Request-Id"] || objectValue(response.headers)["Request-ID"] || "-";
  const statusText = stepResult?.status || stepRun?.run?.status || "no run";

  if (!stepResult) {
    return (
      <section className="workflow-step-detail-card">
        <div className="section-head">
          <h2>{stepCopy(step, "runEvidenceTitle", "Step 运行证据")}</h2>
          <span className="evidence-count">{stepRun?.run?.id || "no run"}</span>
        </div>
        <p className="dashboard-empty">{stepCopy(step, "emptyRun", "还没有这个 Step 的运行记录。")}</p>
      </section>
    );
  }

  return (
    <section className="workflow-step-run-evidence" aria-label={stepCopy(step, "runEvidenceTitle", "Step 运行证据")}>
      <div className="section-head">
        <div>
          <h2>{stepCopy(step, "runEvidenceTitle", "Step 运行证据")}</h2>
          <p>{stepRun?.run?.id || "no run"}</p>
        </div>
        <span className={`status-pill ${String(statusText).toLowerCase() === "passed" ? "passed" : "idle"}`}>{statusText}</span>
      </div>

      <div className="workflow-step-run-summary">
        <article><span>step</span><strong>{stepResult.stepId || "-"}</strong></article>
        <article><span>case</span><strong>{stepResult.caseId || "-"}</strong></article>
        <article><span>http</span><strong>{objectValue(response).statusCode || objectValue(stepResult.summary).httpCode || "-"}</strong></article>
        <article><span>request id</span><strong>{requestID}</strong></article>
        <article><span>timeout</span><strong>{formatMs(step?.timeoutMs || 0)}</strong></article>
        <article><span>elapsed</span><strong>{formatMs(stepResult.elapsedMs ?? objectValue(response).elapsedMs ?? 0)}</strong></article>
      </div>

      <section className="workflow-step-topology-graph">
        <div className="section-head workflow-step-topology-head">
          <div>
            <h2>{stepCopy(step, "topologyTitle", "SkyWalking 自动拓扑")}</h2>
            <p>{`${nodes.length} nodes · ${edges.length} edges · ${topology.status || "unavailable"}`}</p>
          </div>
          {topology.traceId ? <code>{topology.traceId}</code> : null}
        </div>
        <TopologyDiagram topology={topology} markerPrefix={`workflow-step-${stepResult.stepId || "current"}`} emptyLabel={stepCopy(step, "topologyEmpty", "SkyWalking 暂无可绘制链路。")} />
        <div className="workflow-step-topology-edges">
          {edges.length ? edges.map((edge, index) => (
            <article className={`workflow-step-topology-edge ${edge.kind || ""}`} key={`${edge.source}-${edge.target}-${index}`}>
              <strong>{edge.source || "-"}</strong>
              <span>{"->"}</span>
              <strong>{edge.target || "-"}</strong>
              <code>{edge.component || edge.sourceComponent || edge.kind || "-"}</code>
            </article>
          )) : <p className="dashboard-empty">{stepCopy(step, "topologyEdgesEmpty", "当前 step 没有 SkyWalking 边。")}</p>}
        </div>
      </section>

      <section className="workflow-step-request-response">
        <article>
          <div className="section-head">
            <h2>{stepCopy(step, "requestTitle", "请求参数")}</h2>
            <span className="evidence-count">{objectValue(request).method || "-"}</span>
          </div>
          <pre data-smoke-id="step-request">{prettyEvidence(request)}</pre>
        </article>
        <article>
          <div className="section-head">
            <h2>{stepCopy(step, "responseTitle", "返回参数")}</h2>
            <span className="evidence-count">{objectValue(response).statusCode || "-"}</span>
          </div>
          <pre data-smoke-id="step-response">{prettyEvidence(response)}</pre>
        </article>
      </section>

      <section className="workflow-step-logs">
        <div className="section-head">
          <div>
            <h2>{stepCopy(step, "logsTitle", "日志（按服务）")}</h2>
            <p>{`${systems.filter((system) => system.found).length}/${systems.length} services matched`}</p>
          </div>
        </div>
        <div className="workflow-step-log-grid">
          {systems.length ? systems.map((system) => {
            const lines = logLines(system);
            return (
              <article className={`workflow-step-log-system ${system.found ? "found" : "missing"}`} key={logSystemName(system)}>
                <div className="workflow-step-log-head">
                  <strong>{logSystemName(system)}</strong>
                  <span className={`status-pill ${system.found ? "passed" : "idle"}`}>{system.found ? "matched" : "missing"}</span>
                </div>
                <div className="workflow-detail-chips">
                  {(system.matchedKeywords || []).slice(0, 6).map((keyword) => <Chip key={`${logSystemName(system)}-${keyword}`}>{keyword}</Chip>)}
                </div>
                <pre className="workflow-step-log-lines">{lines.length ? lines.join("\n") : system.note || stepCopy(step, "logLinesEmpty", "No matching logs")}</pre>
              </article>
            );
          }) : <p className="dashboard-empty">{stepCopy(step, "logsEmpty", "当前 step 没有日志证据。")}</p>}
        </div>
      </section>
    </section>
  );
}

function WorkflowStepApp() {
  const [catalog, setCatalog] = useState(null);
  const [dashboard, setDashboard] = useState(null);
  const [stepRun, setStepRun] = useState(null);
  const [stepRunMessage, setStepRunMessage] = useState("no run");
  const [stepRunRefresh, setStepRunRefresh] = useState(0);
  const [message, setMessage] = useState("loading");
  const [workflowID, setWorkflowID] = useState(workflowIdFromURL());
  const [stepID, setStepID] = useState(queryParam("step"));
  const runID = queryParam("runId");

  async function refresh() {
    setMessage("loading");
    try {
      const [nextCatalog, nextDashboard] = await Promise.all([fetchJSON("/api/catalog"), fetchJSON("/api/dashboard")]);
      setCatalog(nextCatalog);
      setDashboard(nextDashboard);
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  useEffect(() => {
    setStepRunRefresh(0);
  }, [workflowID, stepID, runID]);

  const workflow = selectedWorkflow(catalog, workflowID);
  const step = selectedStep(workflow, stepID);
  const steps = workflow?.steps || [];
  const foundIndex = steps.findIndex((item) => item.id === step?.id);
  const position = foundIndex >= 0 ? foundIndex : 0;
  const positionText = steps.length ? `${position + 1}/${steps.length}` : "0/0";
  const previous = steps[position - 1];
  const next = steps[position + 1];
  const services = catalog?.services || [];
  const runtime = useMemo(() => {
    const items = (dashboard?.groups || []).flatMap((group) => group.items || []);
    return items.find((item) => item.id === step?.serviceId);
  }, [dashboard, step]);
  const service = services.find((item) => item.id === step?.serviceId);
  const stepResult = stepRun?.summary?.steps?.[0] || null;

  useEffect(() => {
    async function loadStepRun() {
      if (!workflow?.id || !step?.id) {
        setStepRun(null);
        setStepRunMessage("no run");
        return;
      }
      const path = runID
        ? `/api/workflow-runs/step?runId=${encodeURIComponent(runID)}&stepId=${encodeURIComponent(step.id)}`
        : `/api/workflow-runs/latest-step?workflowId=${encodeURIComponent(workflow.id)}&stepId=${encodeURIComponent(step.id)}`;
      setStepRunMessage("loading");
      try {
        const payload = await fetchJSON(path);
        setStepRun(payload);
        setStepRunMessage(payload.run?.status || "loaded");
      } catch {
        setStepRun(null);
        setStepRunMessage("no run");
      }
    }
    loadStepRun();
  }, [workflow?.id, step?.id, runID, stepRunRefresh]);

  useEffect(() => {
    if (!stepRunNeedsRefresh(stepRun) || stepRunRefresh >= 12) return undefined;
    const timer = window.setTimeout(() => setStepRunRefresh((value) => value + 1), 1000);
    return () => window.clearTimeout(timer);
  }, [stepRun, stepRunRefresh]);

  return (
    <main className="app workflow-step-page workflow-step-compact-density" data-template-id="TPL-INTERFACE-STEP-DETAIL-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-INTERFACE-STEP-DETAIL-V1</div>
      <section className="topbar workflow-step-topbar">
        <div>
          <h1>{step?.displayName || step?.id || stepCopy(step, "pageTitle", "Workflow Step 详情")}</h1>
          <p>{workflow ? `${workflow.displayName || workflow.id} · ${positionText}` : "loading"}</p>
        </div>
        <div className="actions">
          <span className="workflow-step-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">{stepCopy(step, "consoleLink", "控制台")}</a>
          <a className="button-link" href="/workflows.html">{stepCopy(step, "workflowDirectoryLink", "Workflow 目录")}</a>
          <a className="button-link" href="/dashboard.html">{stepCopy(step, "dashboardLink", "环境大盘")}</a>
          <a className="button-link" href={`/workflow-detail.html?id=${encodeURIComponent(workflow?.id || "")}`}>{stepCopy(step, "backWorkflowLink", "返回 Workflow 定义")}</a>
        </div>
      </section>

      <section className="workflow-step-load-progress" aria-label="Workflow Step 加载进度" aria-live="polite">
        <div className="workflow-step-load-progress-head">
          <strong>{message === "ready" ? stepCopy(step, "loadedLabel", "已加载") : stepCopy(step, "loadingLabel", "准备加载")}</strong>
          <span>{message === "ready" ? "100%" : "0%"}</span>
        </div>
        <div className="workflow-step-load-progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={message === "ready" ? 100 : 0}>
          <div className="workflow-step-load-progress-fill" style={{ width: message === "ready" ? "100%" : "0%" }} />
        </div>
      </section>

      <section className="workflow-step-layout">
        <aside className="workflow-step-side">
          <h2>{stepCopy(step, "locationTitle", "定位")}</h2>
          <label className="workflow-detail-selector">
            <span>{stepCopy(step, "switchStepLabel", "切换步骤")}</span>
            <select value={step?.id || ""} onChange={(event) => setStepID(event.target.value)}>
              {steps.map((item) => <option value={item.id} key={item.id}>{item.displayName || item.id}</option>)}
            </select>
          </label>
          <code>{step?.id || "-"}</code>
          <h2>{stepCopy(step, "workflowLabel", "Workflow")}</h2>
          <select value={workflow?.id || ""} onChange={(event) => setWorkflowID(event.target.value)}>
            {(catalog?.workflows || []).map((item) => <option value={item.id} key={item.id}>{item.displayName || item.id}</option>)}
          </select>
          <h2>{stepCopy(step, "runEvidenceNavTitle", "运行证据")}</h2>
          <code>{stepRun?.run?.id || runID || stepCopy(step, "unboundRunLabel", "未绑定 run")}</code>
          <span className="workflow-step-status-pill" role="status">{stepRunMessage}</span>
          {(stepRun?.run?.id || runID) && step?.caseId ? (
            <div className="workflow-step-nav">
              <a className="button-link" href={stepEvidenceHref(stepRun?.run?.id || runID, step)}>{stepCopy(step, "runEvidenceLink", "运行证据")}</a>
              <a className="button-link" href={stepTopologyHref(stepRun?.run?.id || runID, step)}>{stepCopy(step, "topologyLink", "调用拓扑")}</a>
            </div>
          ) : null}
          <h2>{stepCopy(step, "stepNavTitle", "前后步骤")}</h2>
          <div className="workflow-step-nav">
            <a className={`button-link ${previous ? "" : "disabled-link"}`} href={previous ? stepHref(workflow.id, previous.id, stepRun?.run?.id || runID) : "#"}>{stepCopy(step, "previousStepLink", "上一步")}</a>
            <a className={`button-link ${next ? "" : "disabled-link"}`} href={next ? stepHref(workflow.id, next.id, stepRun?.run?.id || runID) : "#"}>{stepCopy(step, "nextStepLink", "下一步")}</a>
          </div>
        </aside>

        <section className="workflow-step-main">
          <section className="workflow-step-hero">
            <div>
              <span className="detail-phase">{serviceName(services, step?.serviceId)}</span>
              <h2>{step?.displayName || step?.id || "-"}</h2>
              <p>{[step?.action, runtime?.state, runtime?.health].filter(Boolean).join(" · ") || "-"}</p>
            </div>
            <code>{step?.caseId || "case"}</code>
          </section>
          <section className="workflow-step-grid">
            <article className="workflow-step-card"><span>Action</span><strong>{step?.action || "-"}</strong></article>
            <article className="workflow-step-card"><span>Evidence</span><div className="workflow-detail-chips"><Chip>{runtime?.message || "catalog"}</Chip></div></article>
            <article className="workflow-step-card"><span>Service</span><div className="workflow-detail-chips"><Chip>{step?.serviceId || "-"}</Chip></div></article>
            <article className="workflow-step-card"><span>Max timeout</span><strong>{formatMs(step?.timeoutMs || 0)}</strong></article>
            <article className="workflow-step-card"><span>Latest run</span><strong>{stepRun?.run?.status || "no run"}</strong></article>
          </section>
          <StepContext workflow={workflow} step={step} services={services} />
          <StepTemplateConfig step={step} />
          <ServiceEvidence step={step} service={service} runtime={runtime} />
          <StepEvidenceTemplate stepRun={stepRun} stepResult={stepResult} step={step} />
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-workflow-step-root")).render(<WorkflowStepApp />);
