import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./control-plane-react.css";
import { fetchJSON } from "./api.js";
import { filterWorkflows, workflowKind } from "./workflowModel.js";
import { ButtonLink, Hero, Icons, Panel, Shell, WorkflowCard } from "./components.jsx";

function WorkflowSection({ title, summary, workflows, services }) {
  if (!workflows.length) return null;
  return (
    <section className="react-section">
      <div className="react-section-head">
        <div>
          <h3>{title}</h3>
          <p>{summary}</p>
        </div>
        <span className="react-pill">{workflows.length} entries</span>
      </div>
      <div className="react-workflow-list">
        {workflows.map((workflow) => (
          <WorkflowCard workflow={workflow} services={services} key={workflow.id} />
        ))}
      </div>
    </section>
  );
}

function WorkflowCatalogStudio() {
  const [catalog, setCatalog] = useState(null);
  const [query, setQuery] = useState(new URLSearchParams(window.location.search).get("workflowFilter") || "");
  const [message, setMessage] = useState("loading");
  const [error, setError] = useState("");

  async function refresh() {
    setMessage("loading");
    setError("");
    try {
      setCatalog(await fetchJSON("/api/catalog"));
      setMessage("ready");
    } catch (refreshError) {
      setError(refreshError.message);
      setMessage("failed");
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const workflows = catalog?.workflows || [];
  const services = catalog?.services || [];
  const visible = useMemo(() => filterWorkflows(workflows, services, query), [workflows, services, query]);
  const businessFlows = visible.filter((workflow) => workflowKind(workflow) === "businessFlow");
  const toolEntries = visible.filter((workflow) => workflowKind(workflow) !== "businessFlow");

  return (
    <Shell>
      <Hero
        kicker="React Catalog Studio"
        title="Workflow 清单"
        summary={query ? `${visible.length}/${workflows.length} 个匹配入口` : `${businessFlows.length} 个业务流 · ${toolEntries.length} 个观测/工具入口`}
        actions={
          <>
            <span className="react-status">{message}</span>
            <ButtonLink href="/" icon={Icons.LayoutDashboard}>控制台</ButtonLink>
            <ButtonLink href="/dashboard.html" icon={Icons.Gauge}>环境大盘</ButtonLink>
            <ButtonLink href="/service-inventory.html" icon={Icons.Boxes}>服务清单</ButtonLink>
          </>
        }
        stats={[
          { label: "Business", value: businessFlows.length },
          { label: "Tools", value: toolEntries.length },
          { label: "Services", value: services.length },
          { label: "Catalog", value: catalog?.schemaVersion || "-" },
        ]}
      />

      {error ? <div className="react-error">{error}</div> : null}

      <Panel
        title="Catalog routing"
        label="Workflow map"
        summary="业务流使用 Workflow Studio；平台配置、服务健康、Replay/Probe 保留为控制面工具入口。"
        action={
          <div className="react-toolbar">
            <Icons.Search size={16} aria-hidden="true" />
            <input className="react-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 Workflow / 服务 / Step" />
          </div>
        }
      >
        {visible.length ? (
          <>
            <WorkflowSection
              title="业务流 Workflow"
              summary="可运行的端到端业务链路，适合进入 Workflow Studio。"
              workflows={businessFlows}
              services={services}
            />
            <WorkflowSection
              title="观测/工具入口"
              summary="平台配置、服务健康和 Replay/Probe 等控制面入口，不作为业务流模版展示。"
              workflows={toolEntries}
              services={services}
            />
          </>
        ) : (
          <div className="react-empty">没有匹配的 Workflow。</div>
        )}
      </Panel>
    </Shell>
  );
}

createRoot(document.getElementById("react-workflows-root")).render(<WorkflowCatalogStudio />);

