import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Chip, fetchJSON, selectedWorkflow, serviceName, workflowIdFromURL } from "./workflowPagesCommon.jsx";

function serviceIds(workflow) {
  return [...new Set((workflow?.steps || []).map((step) => step.serviceId).filter(Boolean))];
}

function runStatusTone(status) {
  const value = String(status || "").toLowerCase();
  if (["passed", "success", "ok"].includes(value)) return "passed";
  if (["failed", "error"].includes(value)) return "failed";
  return "idle";
}

function coverageNumber(summary, key) {
  const value = summary?.[key];
  return Number.isFinite(value) ? value : 0;
}

function CoverageCard({ title, value, detail }) {
  return (
    <article className="workflow-coverage-card">
      <strong>{title}</strong>
      <code>{value}</code>
      <span>{detail}</span>
    </article>
  );
}

function InterfaceCoverageRow({ row }) {
  return (
    <article className={`workflow-interface-coverage-row ${row.mapped ? "mapped" : "unmapped"}`}>
      <div className="workflow-interface-coverage-title">
        <strong>{row.stepId || "-"}</strong>
        <code>{row.caseDisplayName || row.caseId || "no case"}</code>
      </div>
      <div className="workflow-interface-coverage-state">
        <span className={`status-pill ${row.mapped ? "passed" : "failed"}`}>{row.mapped ? "mapped" : "gap"}</span>
        <code>{row.admissionStatus || "pending"}</code>
      </div>
      <div className="workflow-interface-coverage-target">
        {row.href ? <a className="button-link" href={row.href}>{row.nodeDisplayName || row.nodeId}</a> : <span>未映射接口节点</span>}
      </div>
    </article>
  );
}

function WorkflowCoverage({ workflow, coverage }) {
  const summary = coverage?.summary || {};
  const rows = coverage?.rows || [];
  return (
    <section className="workflow-coverage-panel">
      <div className="section-head">
        <div>
          <h2>接口覆盖</h2>
          <p>{workflow ? `${workflow.id} · ${coverageNumber(summary, "mappedSteps")}/${coverageNumber(summary, "totalSteps")} mapped` : "loading"}</p>
        </div>
        {workflow?.id ? <a className="button-link" href={`/api/interface-node/coverage-gaps?workflow=${encodeURIComponent(workflow.id)}`}>覆盖缺口 JSON</a> : null}
      </div>
      <div className="workflow-coverage-grid">
        <CoverageCard title="total steps" value={coverageNumber(summary, "totalSteps")} detail="workflow bindings" />
        <CoverageCard title="mapped" value={coverageNumber(summary, "mappedSteps")} detail="interface nodes" />
        <CoverageCard title="unmapped" value={coverageNumber(summary, "unmappedSteps")} detail="coverage gaps" />
        <CoverageCard title="pending" value={coverageNumber(summary, "pendingNodes")} detail="admission state" />
      </div>
      <section className="workflow-interface-coverage">
        <h3>Step interface map</h3>
        <div className="workflow-interface-coverage-list">
          {rows.length ? rows.map((row) => <InterfaceCoverageRow row={row} key={`${row.workflowId}-${row.stepId}`} />) : <p className="dashboard-empty">当前 Workflow 没有接口覆盖记录。</p>}
        </div>
      </section>
    </section>
  );
}

function WorkflowGraph({ workflow, services }) {
  const steps = workflow?.steps || [];
  return (
    <div className="workflow-graph-panel" aria-label="Workflow 链路">
      <div className="workflow-graph-nodes">
        {steps.length ? steps.map((step, index) => (
          <a className="workflow-graph-node service" href={`/workflow-step.html?workflow=${encodeURIComponent(workflow.id)}&step=${encodeURIComponent(step.id)}`} key={step.id}>
            <strong>{step.displayName || step.id}</strong>
            <span>{serviceName(services, step.serviceId)}</span>
            <code>{index + 1}</code>
          </a>
        )) : <p className="dashboard-empty">当前 Workflow 还没有声明步骤。</p>}
      </div>
      <div className="workflow-graph-edges">
        {steps.length > 1 ? steps.slice(1).map((step, index) => (
          <article className="workflow-graph-edge" key={`${steps[index].id}-${step.id}`}>
            <strong>{steps[index].displayName || steps[index].id}</strong>
            <span>{"->"}</span>
            <strong>{step.displayName || step.id}</strong>
          </article>
        )) : <p className="dashboard-empty">需要两个以上步骤才会生成链路边。</p>}
      </div>
    </div>
  );
}

function StepList({ workflow, services }) {
  const steps = workflow?.steps || [];
  return (
    <div className="workflow-detail-steps">
      {steps.length ? steps.map((step, index) => (
        <article className="workflow-detail-step" key={step.id}>
          <div className="workflow-detail-step-top">
            <span>{String(index + 1).padStart(2, "0")}</span>
            <strong>{step.displayName || step.id}</strong>
            <code>{step.required ? "required" : "optional"}</code>
          </div>
          <p>{[serviceName(services, step.serviceId), step.action, step.caseId].filter(Boolean).join(" · ")}</p>
          <div className="workflow-detail-chips">
            <Chip>{step.id}</Chip>
            {step.caseId ? <Chip>{step.caseId}</Chip> : null}
            {step.serviceId ? <Chip>{step.serviceId}</Chip> : null}
          </div>
          <a className="button-link" href={`/workflow-step.html?workflow=${encodeURIComponent(workflow.id)}&step=${encodeURIComponent(step.id)}`}>查看 Step</a>
        </article>
      )) : <p className="dashboard-empty">当前 Workflow 还没有可查看的 Step。</p>}
    </div>
  );
}

function WorkflowDetailApp() {
  const [catalog, setCatalog] = useState(null);
  const [coverage, setCoverage] = useState(null);
  const [message, setMessage] = useState("loading");
  const [workflowID, setWorkflowID] = useState(workflowIdFromURL());

  async function refresh() {
    setMessage("loading");
    try {
      setCatalog(await fetchJSON("/api/catalog"));
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const workflows = catalog?.workflows || [];
  const workflow = selectedWorkflow(catalog, workflowID);
  const services = catalog?.services || [];
  const covered = useMemo(() => serviceIds(workflow), [workflow]);
  const warnings = catalog?.warnings || [];
  const latestRun = workflow?.latestRun || null;
  const latestStatus = latestRun?.status || (workflow?.runCount ? "unknown" : "no run");

  useEffect(() => {
    if (!workflow?.id) {
      setCoverage(null);
      return;
    }
    let cancelled = false;
    fetchJSON(`/api/interface-node/coverage?workflow=${encodeURIComponent(workflow.id)}`)
      .then((payload) => {
        if (!cancelled) setCoverage(payload);
      })
      .catch((error) => {
        if (!cancelled) setCoverage({ ok: false, error: error.message, rows: [], summary: {} });
      });
    return () => {
      cancelled = true;
    };
  }, [workflow?.id]);

  return (
    <main className="app workflow-detail-page workflow-detail-compact-density" data-template-id="TPL-WORKFLOW-LONG-CHAIN-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-WORKFLOW-LONG-CHAIN-V1</div>
      <section className="topbar">
        <div>
          <h1>{workflow?.displayName || workflow?.id || "Workflow 定义"}</h1>
          <p>{workflow ? `${workflow.steps?.length || 0} steps · ${covered.length} services` : "loading"}</p>
        </div>
        <div className="actions">
          <span className="workflow-detail-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">控制台</a>
          <a className="button-link" href="/workflows.html">Workflow 目录</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <a className="primary-action" href={`/workflow-run.html?workflow=${encodeURIComponent(workflow?.id || "")}`}>运行记录</a>
        </div>
      </section>

      <section className="workflow-run-template" aria-label="Workflow run template">
        <div className="workflow-run-template-head">
          <article><span>workflow</span><strong>{workflow?.id || "-"}</strong></article>
          <article><span>steps</span><strong>{workflow?.steps?.length || 0}</strong></article>
          <article><span>runs</span><strong>{workflow?.runCount || 0}</strong></article>
          <article><span>status</span><strong className={`status-pill ${runStatusTone(latestStatus)}`}>{latestStatus}</strong></article>
          <article><span>source</span><strong>{catalog?.source?.kind || "-"}</strong></article>
        </div>
      </section>

      <section className="workflow-detail-layout">
        <aside className="workflow-detail-side">
          <h2>定义来源</h2>
          <p>{catalog?.source?.kind || "loading"}</p>
          {warnings.length ? <div className="workflow-detail-warning">{warnings.join(" · ")}</div> : null}
          <h2>Workflow</h2>
          <label className="workflow-detail-selector">
            <span>切换 Workflow</span>
            <select value={workflow?.id || ""} onChange={(event) => setWorkflowID(event.target.value)}>
              {workflows.map((item) => <option value={item.id} key={item.id}>{item.displayName || item.id}</option>)}
            </select>
          </label>
          <code>{workflow?.id || "-"}</code>
          <h2>模板入口</h2>
          <code>{workflow?.entrypoint || "-"}</code>
          <h2>服务覆盖</h2>
          <div className="workflow-service-summary">
            {covered.map((serviceId) => <Chip key={serviceId}>{serviceName(services, serviceId)}</Chip>)}
          </div>
        </aside>
        <section className="workflow-detail-main">
          <WorkflowGraph workflow={workflow} services={services} />
          <WorkflowCoverage workflow={workflow} coverage={coverage} />
          <div className="section-head">
            <div>
              <h2>步骤</h2>
              <p>{workflow ? `${workflow.steps?.length || 0} steps` : "loading"}</p>
            </div>
          </div>
          <StepList workflow={workflow} services={services} />
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-workflow-detail-root")).render(<WorkflowDetailApp />);
