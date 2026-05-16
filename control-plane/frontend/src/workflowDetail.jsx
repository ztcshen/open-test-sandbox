import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Chip, fetchJSON, selectedWorkflow, serviceName, workflowIdFromURL } from "./workflowPagesCommon.jsx";

function serviceIds(workflow) {
  return [...new Set((workflow?.steps || []).map((step) => step.serviceId).filter(Boolean))];
}

async function postJSON(path, payload) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || response.statusText);
  return body;
}

function runStatusTone(status) {
  const value = String(status || "").toLowerCase();
  if (["passed", "success", "ok"].includes(value)) return "passed";
  if (["failed", "error"].includes(value)) return "failed";
  return "idle";
}

function resultOK(result) {
  return Boolean(result?.ok) && result?.bodyHealth?.ok !== false;
}

function runStepId(step) {
  return step?.stepId || step?.id || "";
}

function resultBodyHealth(result) {
  if (result?.bodyHealth) return result.bodyHealth;
  const reason = result?.error || result?.summary?.failureReason || "";
  return { ok: Boolean(result?.ok), level: result?.ok ? "ok" : "failed", message: result?.ok ? "" : reason || "case failed" };
}

function unsupportedStepResult(step) {
  return {
    ok: false,
    stepOk: false,
    status: "failed",
    caseId: step.caseId || "",
    stepId: step.id,
    title: step.displayName || step.id,
    elapsedMs: 0,
    summary: { failureReason: "caseId is required" },
    bodyHealth: { ok: false, level: "failed", message: "caseId is required" },
  };
}

function parseBody(value) {
  if (value === undefined || value === null || value === "") return {};
  if (typeof value === "object") return value;
  if (typeof value !== "string") return {};
  try {
    return JSON.parse(value);
  } catch {
    return {};
  }
}

function valueAtPath(root, path) {
  if (!path) return undefined;
  return String(path).split(".").reduce((current, part) => {
    if (current === undefined || current === null) return undefined;
    if (Array.isArray(current) && /^\d+$/.test(part)) return current[Number(part)];
    return current[part];
  }, root);
}

function requestEvidence(result) {
  return result?.result?.request || {};
}

function responseEvidence(result) {
  return result?.result?.response || {};
}

function exportRoot(result, source) {
  const request = requestEvidence(result);
  const response = responseEvidence(result);
  const responseBody = parseBody(response.body);
  switch (source) {
    case "request":
    case "requestBody":
      return request.body || {};
    case "requestQuery":
      return request.query || {};
    case "response":
    case "responseBody":
      return responseBody;
    case "responseHeaders":
      return response.headers || {};
    default:
      return responseBody;
  }
}

function exportedValues(step, result) {
  const out = {};
  for (const item of step?.exports || []) {
    const name = item?.name;
    const value = valueAtPath(exportRoot(result, item?.from), item?.path);
    if (name && value !== undefined && value !== null && value !== "") {
      out[name] = value;
    }
  }
  return out;
}

function workflowRunSnapshot(workflow, steps, startedAt, done) {
  const passed = steps.filter(resultOK).length;
  const ok = done && steps.length === (workflow?.steps || []).length && passed === steps.length;
  const elapsedMs = Date.now() - startedAt;
  return {
    workflowId: workflow?.id || "",
    status: done ? (ok ? "passed" : "failed") : "running",
    ok,
    elapsedMs,
    summary: { expectedStepCount: workflow?.steps?.length || 0, stepCount: steps.length, passed, elapsedMs, timeoutMs: workflowTimeoutMs(workflow) },
    steps,
  };
}

function cachedRunState(latestRun) {
  const summary = latestRun?.summary || {};
  const nested = summary.summary || {};
  const steps = Array.isArray(summary.steps) ? summary.steps : [];
  return {
    status: latestRun?.status || summary.status || "idle",
    steps,
    runId: latestRun?.id || "",
    message: steps.length ? "cached run" : "loading cached run",
    elapsedMs: summary.elapsedMs || nested.elapsedMs || 0,
  };
}

function workflowTimeoutMs(workflow) {
  if (workflow?.timeoutMs > 0) return workflow.timeoutMs;
  const base = workflow?.baseStepTimeoutMs > 0 ? workflow.baseStepTimeoutMs : 3000;
  const offset = workflow?.timeoutOffsetMs > 0 ? workflow.timeoutOffsetMs : 0;
  return offset + (workflow?.steps || []).reduce((total, step) => total + (step.timeoutMs > 0 ? step.timeoutMs : base), 0);
}

function formatMs(value) {
  const ms = Number(value);
  if (!Number.isFinite(ms) || ms < 0) return "-";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms % 1000 === 0 ? 0 : 1)} s`;
}

function coverageNumber(summary, key) {
  const value = summary?.[key];
  return Number.isFinite(value) ? value : 0;
}

function workflowCopy(workflow, key, fallback) {
  return workflow?.presentation?.copy?.[key] || fallback;
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

function interfaceContextHref(row, runId = "") {
  if (!row.href) return "";
  const params = new URLSearchParams();
  const [base, query = ""] = row.href.split("?");
  new URLSearchParams(query).forEach((value, key) => params.set(key, value));
  if (row.nodeId && !params.get("id")) params.set("id", row.nodeId);
  if (runId) {
    params.set("runId", runId);
  }
  if (row.workflowId) {
    params.set("flowId", row.workflowId);
    params.set("workflowId", row.workflowId);
  }
  if (row.stepId) params.set("stepId", row.stepId);
  return `${base}?${params.toString()}`;
}

function InterfaceCoverageRow({ row, runId }) {
  const href = interfaceContextHref(row, runId);
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
        {href ? <a className="button-link" href={href}>{row.nodeDisplayName || row.nodeId}</a> : <span>未映射接口节点</span>}
      </div>
    </article>
  );
}

function WorkflowCoverage({ workflow, coverage, runId }) {
  const summary = coverage?.summary || {};
  const rows = coverage?.rows || [];
  return (
    <section className="workflow-coverage-panel">
      <div className="section-head">
        <div>
          <h2>{workflowCopy(workflow, "coverageTitle", "接口覆盖")}</h2>
          <p>{workflow ? `${workflow.id} · ${coverageNumber(summary, "mappedSteps")}/${coverageNumber(summary, "totalSteps")} mapped` : "loading"}</p>
        </div>
        {workflow?.id ? <a className="button-link" href={`/api/interface-node/coverage-gaps?workflow=${encodeURIComponent(workflow.id)}`}>{workflowCopy(workflow, "coverageGapsLink", "覆盖缺口 JSON")}</a> : null}
      </div>
      <div className="workflow-coverage-grid">
        <CoverageCard title="total steps" value={coverageNumber(summary, "totalSteps")} detail="workflow bindings" />
        <CoverageCard title="mapped" value={coverageNumber(summary, "mappedSteps")} detail="interface nodes" />
        <CoverageCard title="unmapped" value={coverageNumber(summary, "unmappedSteps")} detail="coverage gaps" />
        <CoverageCard title="pending" value={coverageNumber(summary, "pendingNodes")} detail="admission state" />
      </div>
      <section className="workflow-interface-coverage">
        <h3>{workflowCopy(workflow, "coverageMapTitle", "Step interface map")}</h3>
        <div className="workflow-interface-coverage-list">
          {rows.length ? rows.map((row) => <InterfaceCoverageRow row={row} runId={runId} key={`${row.workflowId}-${row.stepId}`} />) : <p className="dashboard-empty">{workflowCopy(workflow, "coverageEmpty", "当前 Workflow 没有接口覆盖记录。")}</p>}
        </div>
      </section>
    </section>
  );
}

function workflowStepHref(workflow, step, runId = "") {
  const params = new URLSearchParams({
    workflow: workflow?.id || "",
    step: step?.id || "",
  });
  if (runId) params.set("runId", runId);
  return `/workflow-step.html?${params.toString()}`;
}

function WorkflowRunner({ workflow, state, onRun }) {
  const steps = workflow?.steps || [];
  const results = state.steps || [];
  const resultByStep = new Map(results.map((item) => [runStepId(item), item]));
  const percent = steps.length ? Math.round((results.length / steps.length) * 100) : 0;
  const running = state.status === "running";
  const elapsedMs = state.startedAt ? Date.now() - state.startedAt : state.elapsedMs;
  const timeoutMs = workflowTimeoutMs(workflow);
  return (
    <section className="workflow-progress" aria-label="Workflow runner">
      <div className="workflow-progress-head">
        <span>{`${results.length} / ${steps.length || 0}`}</span>
        <strong>{state.message || "等待运行"} · {formatMs(elapsedMs || 0)} / {formatMs(timeoutMs)}</strong>
      </div>
      <div className="workflow-progress-track" aria-hidden="true"><div className="workflow-progress-fill" style={{ width: `${percent}%` }} /></div>
      <div className="workflow-progress-steps">
        {steps.length ? steps.map((step, index) => {
          const result = resultByStep.get(step.id);
          const tone = result ? (resultOK(result) ? "passed" : "failed") : running && results.length === index ? "running" : "";
          return (
            <a className={`workflow-progress-step ${tone}`} href={workflowStepHref(workflow, step, state.runId || "")} key={step.id}>
              <span className="workflow-progress-index">{String(index + 1).padStart(2, "0")}</span>
              <strong className="workflow-progress-title">{step.displayName || step.id}</strong>
              <em>{formatMs(result?.elapsedMs || 0)} / {formatMs(step.timeoutMs || workflow?.baseStepTimeoutMs || 3000)}</em>
            </a>
          );
        }) : <p className="dashboard-empty">当前 Workflow 没有可运行步骤。</p>}
      </div>
      <div className="actions">
        <button className="primary-action" type="button" disabled={running || !steps.length} onClick={onRun}>{running ? workflowCopy(workflow, "runningButton", "运行中") : workflowCopy(workflow, "runButton", "运行 Workflow")}</button>
        {state.runId ? <a className="button-link" href={`/workflow-run.html?id=${encodeURIComponent(state.runId)}`}>{workflowCopy(workflow, "viewRunLink", "查看运行记录")}</a> : null}
      </div>
    </section>
  );
}

function WorkflowDetailApp() {
  const [catalog, setCatalog] = useState(null);
  const [coverage, setCoverage] = useState(null);
  const [runner, setRunner] = useState({ status: "idle", steps: [], message: "等待运行" });
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
    if (!workflow?.id || !latestRun?.id) {
      setRunner({ status: "idle", steps: [], message: workflow?.id ? "等待运行" : "loading" });
      return;
    }
    let cancelled = false;
    setRunner((current) => {
      if (current.status === "running" || current.runId === latestRun.id) return current;
      return cachedRunState(latestRun);
    });
    fetchJSON(`/api/workflow-runs/${encodeURIComponent(latestRun.id)}`)
      .then((payload) => {
        if (cancelled) return;
        const summary = payload.summary || {};
        setRunner({
          status: payload.run?.status || summary.status || latestRun.status || "idle",
          steps: Array.isArray(summary.steps) ? summary.steps : [],
          runId: latestRun.id,
          message: "cached run",
        });
      })
      .catch((error) => {
        if (!cancelled) setRunner({ status: "idle", steps: [], runId: latestRun.id, message: error.message });
      });
    return () => {
      cancelled = true;
    };
  }, [workflow?.id, latestRun?.id]);

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

  async function runWorkflow() {
    if (!workflow?.id || runner.status === "running") return;
    const startedAt = Date.now();
    const results = [];
    let context = {};
    setRunner({ status: "running", steps: [], message: "starting", startedAt, elapsedMs: 0 });
    try {
      for (const step of workflow.steps || []) {
        const remainingMs = workflowTimeoutMs(workflow) - (Date.now() - startedAt);
        if (remainingMs <= 0) throw new Error(`workflow exceeded ${formatMs(workflowTimeoutMs(workflow))} timeout`);
        setRunner({ status: "running", steps: [...results], message: `running ${step.displayName || step.id}`, startedAt, elapsedMs: Date.now() - startedAt });
        const result = step.caseId
          ? await postJSON("/api/test-kit/run", { caseId: step.caseId, workflowId: workflow.id, stepId: step.id, overrides: context, timeoutSeconds: Math.max(1, Math.ceil(Math.min(remainingMs, step.timeoutMs || workflow?.baseStepTimeoutMs || 3000) / 1000)) })
          : unsupportedStepResult(step);
        const withStep = {
          ...result,
          stepId: step.id,
          title: step.displayName || step.id,
          bodyHealth: resultBodyHealth(result),
          stepOk: resultOK({ ...result, bodyHealth: resultBodyHealth(result) }),
        };
        context = { ...context, ...exportedValues(step, withStep) };
        results.push(withStep);
        setRunner({ status: "running", steps: [...results], message: `completed ${results.length}/${workflow.steps?.length || 0}`, startedAt, elapsedMs: Date.now() - startedAt });
        if (!resultOK(withStep)) break;
      }
      const snapshot = workflowRunSnapshot(workflow, results, startedAt, true);
      const saved = results.length ? await postJSON("/api/workflow-runs", snapshot) : {};
      setRunner({ status: snapshot.status, steps: results, runId: saved.workflowRunId || "", message: snapshot.ok ? "workflow completed" : "workflow failed", elapsedMs: snapshot.elapsedMs });
      refresh();
    } catch (error) {
      setRunner({ status: "failed", steps: results, message: error.message, elapsedMs: Date.now() - startedAt });
    }
  }

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
          <article><span>timeout</span><strong>{formatMs(workflowTimeoutMs(workflow))}</strong></article>
          <article><span>runs</span><strong>{workflow?.runCount || 0}</strong></article>
          <article><span>status</span><strong className={`status-pill ${runStatusTone(latestStatus)}`}>{latestStatus}</strong></article>
          <article><span>source</span><strong>{catalog?.source?.kind || "-"}</strong></article>
        </div>
        <WorkflowRunner workflow={workflow} state={runner} onRun={runWorkflow} />
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
          <WorkflowCoverage workflow={workflow} coverage={coverage} runId={runner.runId || latestRun?.id || ""} />
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-workflow-detail-root")).render(<WorkflowDetailApp />);
