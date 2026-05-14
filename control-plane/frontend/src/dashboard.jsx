import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./control-plane-react.css";
import { classNames, fetchJSON } from "./api.js";
import { filterWorkflows, workflowKind, workflowServiceIds } from "./workflowModel.js";
import { ButtonLink, Hero, IconButton, Icons, Panel, Shell, WorkflowCard } from "./components.jsx";

function statusTone(item) {
  if (!item) return "warn";
  if (item.ok) return "good";
  if (item.state === "missing") return "warn";
  return "bad";
}

function serviceStatusText(item) {
  if (!item) return "catalog";
  if (item.state === "missing") return "未运行";
  if (item.health && item.health !== "unknown") return item.health;
  return item.state || "unknown";
}

function workflowImpact(workflow, statusById) {
  const modeled = workflowServiceIds(workflow).map((id) => statusById.get(id)).filter(Boolean);
  const unhealthy = modeled.filter((item) => !item.ok && item.state !== "missing").length;
  const missing = modeled.filter((item) => item.state === "missing").length;
  if (unhealthy) return { text: `${unhealthy} 异常`, tone: "bad" };
  if (missing) return { text: `${missing} 未运行`, tone: "warn" };
  return { text: "服务正常", tone: "good" };
}

function ExecutiveDashboard() {
  const [snapshot, setSnapshot] = useState(null);
  const [catalog, setCatalog] = useState(null);
  const [runs, setRuns] = useState(null);
  const [query, setQuery] = useState(new URLSearchParams(window.location.search).get("workflowFilter") || "");
  const [message, setMessage] = useState("loading");
  const [error, setError] = useState("");

  async function refresh() {
    setMessage("refreshing");
    setError("");
    try {
      const [nextSnapshot, nextCatalog, nextRuns] = await Promise.all([
        fetchJSON("/api/dashboard"),
        fetchJSON("/api/catalog"),
        fetchJSON("/api/runs"),
      ]);
      setSnapshot(nextSnapshot);
      setCatalog(nextCatalog);
      setRuns(nextRuns);
      setMessage("ready");
    } catch (refreshError) {
      setError(refreshError.message);
      setMessage("failed");
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const services = catalog?.services || [];
  const workflows = catalog?.workflows || [];
  const summary = snapshot?.summary || {};
  const statusById = useMemo(() => {
    const byId = new Map();
    (snapshot?.groups || []).forEach((group) => (group.items || []).forEach((item) => byId.set(item.id, item)));
    return byId;
  }, [snapshot]);
  const filtered = useMemo(() => filterWorkflows(workflows, services, query), [workflows, services, query]);
  const businessFlowCount = workflows.filter((workflow) => workflowKind(workflow) === "businessFlow").length;
  const latestRun = runs?.workflowRuns?.[0];

  return (
    <Shell>
      <Hero
        kicker="React Control Plane"
        title="环境大盘"
        summary={`${summary.healthy || 0}/${summary.total || 0} healthy · ${summary.missing || 0} missing · ${businessFlowCount} 条业务流`}
        actions={
          <>
            <span className={classNames("react-status", message === "failed" && "bad")}>{message}</span>
            <ButtonLink href="/" icon={Icons.LayoutDashboard}>控制台</ButtonLink>
            <ButtonLink href="/environment-nodes.html" icon={Icons.Server}>环境节点</ButtonLink>
            <ButtonLink href="/workflows.html" primary icon={Icons.Workflow}>Workflow 清单</ButtonLink>
            <IconButton icon={Icons.RefreshCw} title="刷新状态" onClick={refresh}>刷新</IconButton>
          </>
        }
        stats={[
          { label: "业务服务", value: (snapshot?.groups || []).find((group) => group.id === "business")?.items?.length || 0 },
          { label: "健康", value: summary.healthy || 0 },
          { label: "缺失", value: summary.missing || 0 },
          { label: "异常", value: summary.unhealthy || 0 },
        ]}
      />

      {error ? <div className="react-error">{error}</div> : null}

      <section className="react-grid">
        <Panel
          title="服务拓扑"
          label="Runtime map"
          summary={`${services.length || 0} 个服务节点 · ${catalog?.topology?.edges?.length || 0} 条真实边`}
          action={<span className="react-pill good">{catalog?.source?.kind === "manifest" ? `Manifest v${catalog.schemaVersion || "-"}` : "Catalog"}</span>}
        >
          <div className="react-node-grid">
            {services.map((service) => {
              const runtime = statusById.get(service.id);
              const usage = workflows.filter((workflow) => workflowServiceIds(workflow).includes(service.id)).length;
              return (
                <article className="react-card" key={service.id}>
                  <div className="react-card-top">
                    <a className="react-card-title" href={service.role === "external" ? "/service-inventory.html" : `/environment-node.html?id=${encodeURIComponent(service.id)}`}>
                      {service.displayName || service.id}
                    </a>
                    <span className={classNames("react-pill", statusTone(runtime))}>{serviceStatusText(runtime)}</span>
                  </div>
                  <p>{[service.role, service.port ? `:${service.port}` : "", service.dependencies?.length ? `下游 ${service.dependencies.length}` : "末端节点"].filter(Boolean).join(" · ")}</p>
                  <div className="react-service-chips">
                    <button className="react-chip" type="button" onClick={() => setQuery(service.id)}>Workflow 使用: {usage}</button>
                  </div>
                </article>
              );
            })}
          </div>
        </Panel>

        <Panel
          title="Workflow 目录"
          label="Catalog"
          summary={query ? `${filtered.length}/${workflows.length} 个匹配` : `${workflows.length} 个入口 · 按服务和运行态筛选`}
          action={
            <div className="react-toolbar">
              <Icons.Search size={16} aria-hidden="true" />
              <input className="react-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 Workflow / 服务 / 状态" />
            </div>
          }
        >
          <div className="react-workflow-list">
            {filtered.length ? filtered.map((workflow) => (
              <WorkflowCard workflow={workflow} services={services} compact key={workflow.id} />
            )) : <div className="react-empty">没有匹配的 Workflow。</div>}
          </div>
        </Panel>
      </section>

      <Panel
        title="最近运行"
        label="Evidence"
        summary={latestRun ? `${latestRun.workflowId || "-"} · ${latestRun.status || "-"}` : "暂无运行记录"}
        dark
        action={<ButtonLink href="/workflow-run.html" icon={Icons.Activity}>查看运行历史</ButtonLink>}
      >
        <div className="react-stat-grid">
          <article className="react-stat"><span>Replay</span><strong>{runs?.replayRuns?.length || 0}</strong></article>
          <article className="react-stat"><span>Workflow</span><strong>{runs?.workflowRuns?.length || 0}</strong></article>
          <article className="react-stat"><span>Probe</span><strong>{runs?.probeRuns?.length || 0}</strong></article>
          <article className="react-stat"><span>Status</span><strong>{latestRun?.status || "-"}</strong></article>
        </div>
      </Panel>
    </Shell>
  );
}

createRoot(document.getElementById("react-dashboard-root")).render(<ExecutiveDashboard />);
