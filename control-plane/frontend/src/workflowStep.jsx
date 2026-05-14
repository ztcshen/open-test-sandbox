import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Chip, fetchJSON, queryParam, selectedStep, selectedWorkflow, serviceName, workflowIdFromURL } from "./workflowPagesCommon.jsx";

function unique(values) {
  return [...new Set((values || []).filter(Boolean))];
}

function runtimeText(runtime) {
  if (!runtime) return "missing";
  return [runtime.state || "unknown", runtime.health || runtime.message || ""].filter(Boolean).join(" · ");
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

function StepContext({ workflow, step, services }) {
  const steps = workflow?.steps || [];
  const index = steps.findIndex((item) => item.id === step?.id);
  return (
    <section className="workflow-step-context">
      <div className="section-head">
        <div>
          <h2>上下文摘要</h2>
          <p>{workflow && step ? `${Math.max(index, 0) + 1} / ${steps.length || 0} · ${workflow.displayName || workflow.id}` : "loading"}</p>
        </div>
      </div>
      <div className="workflow-step-context-grid">
        <ContextCard title="当前服务" values={[serviceName(services, step?.serviceId)]} empty="未声明服务" />
        <ContextCard title="Workflow action" values={steps.map((item) => item.action)} empty="无 action" />
        <ContextCard title="Workflow evidence" values={steps.flatMap((item) => item.evidenceKinds || [])} empty="无 Evidence" />
        <ContextCard title="Workflow cases" values={steps.map((item) => item.caseId)} empty="无 case" />
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
          <h2>服务证据</h2>
          <p>{service ? `${service.displayName || service.id} · ${service.role || "service"} · ${runtimeText(runtime)}` : `${step?.serviceId || "-"} · 未建模`}</p>
        </div>
        <div className="workflow-step-service-actions">
          {step?.serviceId ? <a className="button-link" href={`/environment-node.html?id=${encodeURIComponent(step.serviceId)}`}>环境节点详情</a> : null}
          <a className="button-link" href="/service-inventory.html">服务清单</a>
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

function WorkflowStepApp() {
  const [catalog, setCatalog] = useState(null);
  const [dashboard, setDashboard] = useState(null);
  const [stepRun, setStepRun] = useState(null);
  const [stepRunMessage, setStepRunMessage] = useState("no run");
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
  }, [workflow?.id, step?.id, runID]);

  return (
    <main className="app workflow-step-page workflow-step-compact-density" data-template-id="TPL-INTERFACE-STEP-DETAIL-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-INTERFACE-STEP-DETAIL-V1</div>
      <section className="topbar workflow-step-topbar">
        <div>
          <h1>{step?.displayName || step?.id || "Workflow Step 详情"}</h1>
          <p>{workflow ? `${workflow.displayName || workflow.id} · ${positionText}` : "loading"}</p>
        </div>
        <div className="actions">
          <span className="workflow-step-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">控制台</a>
          <a className="button-link" href="/workflows.html">Workflow 目录</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <a className="button-link" href={`/workflow-detail.html?id=${encodeURIComponent(workflow?.id || "")}`}>返回 Workflow 定义</a>
        </div>
      </section>

      <section className="workflow-step-load-progress" aria-label="Workflow Step 加载进度" aria-live="polite">
        <div className="workflow-step-load-progress-head">
          <strong>{message === "ready" ? "已加载" : "准备加载"}</strong>
          <span>{message === "ready" ? "100%" : "0%"}</span>
        </div>
        <div className="workflow-step-load-progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={message === "ready" ? 100 : 0}>
          <div className="workflow-step-load-progress-fill" style={{ width: message === "ready" ? "100%" : "0%" }} />
        </div>
      </section>

      <section className="workflow-step-layout">
        <aside className="workflow-step-side">
          <h2>定位</h2>
          <label className="workflow-detail-selector">
            <span>切换步骤</span>
            <select value={step?.id || ""} onChange={(event) => setStepID(event.target.value)}>
              {steps.map((item) => <option value={item.id} key={item.id}>{item.displayName || item.id}</option>)}
            </select>
          </label>
          <code>{step?.id || "-"}</code>
          <h2>Workflow</h2>
          <select value={workflow?.id || ""} onChange={(event) => setWorkflowID(event.target.value)}>
            {(catalog?.workflows || []).map((item) => <option value={item.id} key={item.id}>{item.displayName || item.id}</option>)}
          </select>
          <h2>运行证据</h2>
          <code>{stepRun?.run?.id || runID || "未绑定 run"}</code>
          <span className="workflow-step-status-pill" role="status">{stepRunMessage}</span>
          <h2>前后步骤</h2>
          <div className="workflow-step-nav">
            <a className={`button-link ${previous ? "" : "disabled-link"}`} href={previous ? `/workflow-step.html?workflow=${encodeURIComponent(workflow.id)}&step=${encodeURIComponent(previous.id)}` : "#"}>上一步</a>
            <a className={`button-link ${next ? "" : "disabled-link"}`} href={next ? `/workflow-step.html?workflow=${encodeURIComponent(workflow.id)}&step=${encodeURIComponent(next.id)}` : "#"}>下一步</a>
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
            <article className="workflow-step-card"><span>Latest run</span><strong>{stepRun?.run?.status || "no run"}</strong></article>
          </section>
          <StepContext workflow={workflow} step={step} services={services} />
          <ServiceEvidence step={step} service={service} runtime={runtime} />
          <section className="workflow-step-detail-card">
            <div className="section-head">
              <h2>最近 Step Run</h2>
              <span className="evidence-count">{stepRun?.run?.id || "no run"}</span>
            </div>
            {stepResult ? (
              <div className="workflow-step-context-grid">
                <article className="workflow-step-context-card">
                  <strong>status</strong>
                  <div className="workflow-detail-chips">
                    <Chip>{stepResult.status || stepResult.ok || stepRun?.run?.status || "-"}</Chip>
                    <Chip>{stepResult.stepId || step?.id || "-"}</Chip>
                  </div>
                </article>
                <article className="workflow-step-context-card">
                  <strong>request</strong>
                  <pre>{JSON.stringify(stepResult.request || {}, null, 2)}</pre>
                </article>
                <article className="workflow-step-context-card">
                  <strong>response</strong>
                  <pre>{JSON.stringify(stepResult.response || {}, null, 2)}</pre>
                </article>
              </div>
            ) : (
              <p className="dashboard-empty">还没有这个 Step 的运行记录。</p>
            )}
          </section>
          <section className="workflow-step-detail-card">
            <div className="section-head">
              <h2>全步骤导航</h2>
              <span className="evidence-count">{positionText}</span>
            </div>
            <div className="workflow-step-sequence">
              {steps.length ? steps.map((item, index) => (
                <a className={item.id === step?.id ? "active" : ""} href={`/workflow-step.html?workflow=${encodeURIComponent(workflow?.id || "")}&step=${encodeURIComponent(item.id)}`} key={item.id}>
                  <span>{index + 1}</span>
                  <strong>{item.displayName || item.id}</strong>
                </a>
              )) : <p className="dashboard-empty">当前 Workflow 还没有声明步骤。</p>}
            </div>
          </section>
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-workflow-step-root")).render(<WorkflowStepApp />);
