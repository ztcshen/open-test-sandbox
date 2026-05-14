import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { fetchJSON, statusText, statusTone, StatBox } from "./environmentCommon.jsx";

function nodeHref(item) {
  return `/environment-node.html?id=${encodeURIComponent(item.id || "")}`;
}

function EnvironmentCard({ item }) {
  const meta = [
    item.container,
    item.version ? `版本 ${item.version}` : "",
    item.port ? `:${item.port}` : "",
    item.managementPort ? `mgmt:${item.managementPort}` : "",
  ].filter(Boolean);
  return (
    <a className={`dashboard-card environment-node-card-button ${statusTone(item)}`} href={nodeHref(item)} aria-label={`查看 ${item.name} 服务详情`}>
      <div className="dashboard-card-top">
        <strong>{item.name}</strong>
        <span>{statusText(item)}</span>
      </div>
      <div className="dashboard-card-meta">{meta.join(" · ")}</div>
      <p>{item.message || item.image || "-"}</p>
      <div className="dashboard-card-actions">
        <span className="button-link">查看详情</span>
      </div>
    </a>
  );
}

function EnvironmentGroup({ group }) {
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
          <EnvironmentCard item={item} key={item.id} />
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
          <h1>环境节点</h1>
          <p>{`${summary.healthy || 0}/${summary.total || 0} healthy · ${summary.missing || 0} missing`}</p>
        </div>
        <div className="dashboard-top-stats" aria-label="环境状态摘要">
          <StatBox label="业务服务" value={business?.items?.length || 0} />
          <StatBox label="健康" value={summary.healthy || 0} />
          <StatBox label="缺失" value={summary.missing || 0} />
          <StatBox label="异常" value={summary.unhealthy || 0} />
        </div>
        <div className="actions">
          <span className="environment-status-pill" role="status">{message}</span>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <a className="button-link" href="/">控制台</a>
          <button type="button" title="刷新状态" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>
      <section className="environment-grid" aria-label="环境节点">
        {(snapshot?.groups || []).map((group) => (
          <EnvironmentGroup group={group} key={group.id} />
        ))}
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-environment-nodes-root")).render(<EnvironmentNodesApp />);
