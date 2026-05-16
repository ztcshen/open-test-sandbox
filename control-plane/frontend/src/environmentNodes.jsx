import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { envCopy, fetchJSON, statusText, statusTone, StatBox } from "./environmentCommon.jsx";

function nodeHref(item) {
  return `/environment-node.html?id=${encodeURIComponent(item.id || "")}`;
}

function EnvironmentCard({ item, snapshot }) {
  const meta = [
    item.container,
    item.version ? `${envCopy(snapshot, item, "versionPrefix", "版本")} ${item.version}` : "",
    item.port ? `:${item.port}` : "",
    item.managementPort ? `${envCopy(snapshot, item, "managementPortPrefix", "mgmt")}:${item.managementPort}` : "",
  ].filter(Boolean);
  return (
    <a className={`dashboard-card environment-node-card-button ${statusTone(item)}`} href={nodeHref(item)} aria-label={`${envCopy(snapshot, item, "cardAriaPrefix", "查看")} ${item.name} ${envCopy(snapshot, item, "cardAriaSuffix", "服务详情")}`}>
      <div className="dashboard-card-top">
        <strong>{item.name}</strong>
        <span>{statusText(item)}</span>
      </div>
      <div className="dashboard-card-meta">{meta.join(" · ")}</div>
      <p>{item.message || item.image || "-"}</p>
      <div className="dashboard-card-actions">
        <span className="button-link">{envCopy(snapshot, item, "cardDetailLink", "查看详情")}</span>
      </div>
    </a>
  );
}

function EnvironmentGroup({ group, snapshot }) {
  const items = group.items || [];
  const okCount = items.filter((item) => item.ok).length;
  return (
    <section className="dashboard-group">
      <div className="dashboard-group-head">
        <h2>{group.label}</h2>
        <code>{`${okCount}/${items.length}`}</code>
      </div>
      <div className="dashboard-service-list">
        {items.map((item) => (
          <EnvironmentCard item={item} snapshot={snapshot} key={item.id} />
        ))}
      </div>
    </section>
  );
}

function EnvironmentNodesApp() {
  const [snapshot, setSnapshot] = useState(null);
  const [message, setMessage] = useState("loading");

  async function refresh() {
    setMessage("refreshing...");
    try {
      setSnapshot(await fetchJSON("/api/dashboard"));
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const summary = snapshot?.summary || {};
  const business = (snapshot?.groups || []).find((group) => group.id === "business");
  return (
    <main className="app environment-node-page" data-template-id="TPL-ENVIRONMENT-NODE-LIST-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-ENVIRONMENT-NODE-LIST-V1</div>
      <section className="topbar">
        <div>
          <h1>{envCopy(snapshot, {}, "listTitle", "环境节点")}</h1>
          <p>{`${summary.healthy || 0}/${summary.total || 0} healthy · ${summary.missing || 0} missing`}</p>
        </div>
        <div className="dashboard-top-stats" aria-label={envCopy(snapshot, {}, "listSummaryAriaLabel", "环境状态摘要")}>
          <StatBox label={envCopy(snapshot, {}, "businessServicesStat", "业务服务")} value={business?.items?.length || 0} />
          <StatBox label={envCopy(snapshot, {}, "healthyStat", "健康")} value={summary.healthy || 0} />
          <StatBox label={envCopy(snapshot, {}, "missingStat", "缺失")} value={summary.missing || 0} />
          <StatBox label={envCopy(snapshot, {}, "unhealthyStat", "异常")} value={summary.unhealthy || 0} />
        </div>
        <div className="actions">
          <span className="environment-status-pill" role="status">{message}</span>
          <a className="button-link" href="/dashboard.html">{envCopy(snapshot, {}, "dashboardLink", "环境大盘")}</a>
          <a className="button-link" href="/">{envCopy(snapshot, {}, "consoleLink", "控制台")}</a>
          <button type="button" title={envCopy(snapshot, {}, "refreshTitle", "刷新状态")} onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>{envCopy(snapshot, {}, "refreshLabel", "刷新")}</span>
          </button>
        </div>
      </section>
      <section className="environment-grid" aria-label={envCopy(snapshot, {}, "environmentGridLabel", "环境节点")}>
        {(snapshot?.groups || []).map((group) => (
          <EnvironmentGroup group={group} snapshot={snapshot || {}} key={group.id} />
        ))}
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-environment-nodes-root")).render(<EnvironmentNodesApp />);
