import { createRoot } from "react-dom/client";
import { useEffect, useMemo, useState } from "react";
import { fetchJSON } from "./api.js";
import { ButtonLink, Hero, Icons, Panel, Shell } from "./components.jsx";

const pageCopy = {
  "/trace-call.html": {
    title: "调用证据",
    kicker: "Trace Call",
    summary: "查看单次 Workflow step 的请求、关联字段和日志线索。",
    templateId: "TPL-TRACE-CALL-V1",
  },
  "/trace-evidence.html": {
    title: "日志证据",
    kicker: "Trace Evidence",
    summary: "汇总当前 Workflow 的日志关联字段、步骤线索和可回查入口。",
    templateId: "TPL-TRACE-EVIDENCE-V1",
  },
};

function useControlPlaneData() {
  const [state, setState] = useState({ catalog: null, runs: null, status: "loading", error: "" });

  async function refresh() {
    setState((current) => ({ ...current, status: "loading", error: "" }));
    try {
      const [catalog, runs] = await Promise.all([fetchJSON("/api/catalog"), fetchJSON("/api/runs")]);
      setState({ catalog, runs, status: "ready", error: "" });
    } catch (error) {
      setState({ catalog: null, runs: null, status: "failed", error: error.message });
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  return { ...state, refresh };
}

function traceContext(catalog) {
  const params = new URLSearchParams(window.location.search);
  const workflowId = params.get("workflow") || "";
  const stepId = params.get("step") || "";
  const workflow = (catalog?.workflows || []).find((item) => item.id === workflowId) || (catalog?.workflows || [])[0] || null;
  const step = (workflow?.steps || []).find((item) => item.id === stepId) || null;
  return { workflow, step, workflowId, stepId };
}

function EvidenceSummary({ catalog, runs }) {
  const { workflow, step, workflowId, stepId } = traceContext(catalog);
  const workflowRuns = runs?.workflowRuns || [];
  const latest = workflowRuns[0];
  const cards = [
    ["Workflow", workflow?.displayName || workflowId || "-"],
    ["Step", step?.displayName || stepId || "-"],
    ["Runs", String(workflowRuns.length)],
    ["Latest", latest?.status || "-"],
  ];
  return (
    <div className="react-stat-grid">
      {cards.map(([label, value]) => (
        <article className="react-stat" key={label}>
          <span>{label}</span>
          <strong>{value}</strong>
        </article>
      ))}
    </div>
  );
}

function TraceCallBody({ catalog, runs }) {
  const { workflow, step, workflowId, stepId } = traceContext(catalog);
  return (
    <section className="react-grid">
      <Panel title="调用上下文" label="Step" summary="当前页面只展示已登记的 Workflow 和 Step 线索。">
        <EvidenceSummary catalog={catalog} runs={runs} />
        <div className="react-card-actions">
          <ButtonLink href={`/workflow-detail.html?id=${encodeURIComponent(workflow?.id || workflowId || "")}`}>
            Workflow 定义
          </ButtonLink>
          <ButtonLink href={`/workflow-step.html?workflow=${encodeURIComponent(workflow?.id || workflowId || "")}&step=${encodeURIComponent(step?.id || stepId || "")}`} primary>
            Step 详情
          </ButtonLink>
        </div>
      </Panel>
      <Panel title="日志线索" label="Trace" summary="真实日志证据将在运行记录接入后从 Evidence Store 读取。">
        <div className="react-empty">当前没有可展示的调用日志快照。</div>
      </Panel>
    </section>
  );
}

function TraceEvidenceBody({ catalog, runs }) {
  const workflowRuns = runs?.workflowRuns || [];
  const latest = workflowRuns[0];
  return (
    <section className="react-grid">
      <Panel title="运行摘要" label="Evidence" summary="按 Workflow 运行聚合日志证据入口。">
        <EvidenceSummary catalog={catalog} runs={runs} />
        <div className="react-card-actions">
          <ButtonLink href={latest?.id ? `/workflow-run.html?id=${encodeURIComponent(latest.id)}` : "/workflow-run.html"} primary>
            Workflow Run
          </ButtonLink>
          <ButtonLink href={latest?.id ? `/trace-topology.html?workflowRunId=${encodeURIComponent(latest.id)}` : "/trace-topology.html"}>
            Trace Topology
          </ButtonLink>
        </div>
      </Panel>
      <Panel title="日志证据" label="Trace" summary="运行后展示 request id、correlators 和系统日志摘要。">
        <div className="react-empty">当前没有已持久化的日志证据。</div>
      </Panel>
    </section>
  );
}

function ControlPlaneApp() {
  const copy = pageCopy[window.location.pathname] || {
    title: "Control Plane",
    kicker: "AgentTestBench",
    summary: "Generic local-first workbench surface.",
    templateId: "TPL-CONTROL-PLANE-V1",
  };
  const { catalog, runs, status, error, refresh } = useControlPlaneData();
  const body = useMemo(() => {
    if (window.location.pathname === "/trace-call.html") {
      return <TraceCallBody catalog={catalog} runs={runs} />;
    }
    if (window.location.pathname === "/trace-evidence.html") {
      return <TraceEvidenceBody catalog={catalog} runs={runs} />;
    }
    return <div className="react-empty">No view is registered for this route.</div>;
  }, [catalog, runs]);

  return (
    <Shell>
      <div className="template-watermark" aria-label="模板编号">
        {copy.templateId}
      </div>
      <Hero
        kicker={copy.kicker}
        title={copy.title}
        summary={error || copy.summary}
        actions={
          <>
            <span className="react-status">{status}</span>
            <ButtonLink href="/" icon={Icons.LayoutDashboard}>
              控制台
            </ButtonLink>
            <ButtonLink href="/dashboard.html" icon={Icons.Gauge}>
              环境大盘
            </ButtonLink>
            <button className="react-icon-button" type="button" onClick={refresh}>
              <Icons.RefreshCw size={15} aria-hidden="true" />
              <span>刷新</span>
            </button>
          </>
        }
        stats={[
          { label: "Workflows", value: catalog?.workflows?.length || 0 },
          { label: "Runs", value: runs?.workflowRuns?.length || 0 },
          { label: "Source", value: catalog?.source?.kind || "-" },
        ]}
      />
      {body}
    </Shell>
  );
}

createRoot(document.getElementById("react-control-plane-root")).render(<ControlPlaneApp />);
