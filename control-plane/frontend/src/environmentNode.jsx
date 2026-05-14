import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { DetailList, fetchJSON, flattenNodes, runtimeByService, statusText, StatBox } from "./environmentCommon.jsx";

function requestedId() {
  return new URLSearchParams(window.location.search).get("id") || "";
}

function runtimeRows(item, runtime) {
  const role = runtime?.nodeRole || item.group || "";
  const showRepo = Boolean(runtime?.branchName || runtime?.commitId) && !["middleware", "platform", "observability", "external"].includes(role);
  if (showRepo) {
    return [
      ["branch_name", runtime?.branchName || "-"],
      ["commit_id", runtime?.commitId || "-"],
    ];
  }
  return [
    ["image_version", runtime?.imageVersion || item.version || "-"],
    ["image", runtime?.image || item.image || "-"],
    ["container_state", runtime?.containerState || item.state || "-"],
    ["health", runtime?.health || item.health || "-"],
  ];
}

function Panel({ title, summary, className = "", children }) {
  return (
    <section className={`environment-node-detail-panel ${className}`}>
      <div className="dashboard-section-head">
        <div>
          <h2>{title}</h2>
          <p>{summary}</p>
        </div>
      </div>
      {children}
    </section>
  );
}

function ActionLinks({ item }) {
  const links = [
    item.port ? ["打开服务端口", `http://127.0.0.1:${item.port}`] : null,
    item.managementPort ? ["打开管理端口", `http://127.0.0.1:${item.managementPort}`] : null,
  ].filter(Boolean);
  return (
    <div className="environment-node-detail-actions">
      {links.length ? (
        links.map(([label, href]) => (
          <a className="button-link" href={href} target="_blank" rel="noreferrer" key={label}>{label}</a>
        ))
      ) : (
        <p className="dashboard-empty">当前快照没有可打开入口。</p>
      )}
    </div>
  );
}

function PeerList({ item, nodes }) {
  return (
    <Panel title="同组节点" summary="同一环境分组里的当前服务状态" className="environment-node-peers-panel">
      <div className="environment-node-peer-list">
        {nodes.filter((candidate) => candidate.groupId === item.groupId).map((candidate) => (
          <a className={`environment-node-peer ${candidate.id === item.id ? "active" : ""}`} href={`/environment-node.html?id=${encodeURIComponent(candidate.id)}`} key={candidate.id}>
            <strong>{candidate.name || candidate.id}</strong>
            <span>{statusText(candidate)}</span>
          </a>
        ))}
      </div>
    </Panel>
  );
}

function SnapshotSummary({ snapshot }) {
  const summary = snapshot?.summary || {};
  return (
    <Panel title="环境快照" summary="当前 Control plane 看到的全局健康计数" className="environment-node-summary-panel">
      <DetailList rows={[
        ["total", String(summary.total || 0)],
        ["healthy", String(summary.healthy || 0)],
        ["missing", String(summary.missing || 0)],
        ["unhealthy", String(summary.unhealthy || 0)],
      ]} />
    </Panel>
  );
}

function InterfaceLinks({ item }) {
  const [payload, setPayload] = useState(null);
  const [error, setError] = useState("");
  useEffect(() => {
    fetchJSON(`/api/interface-nodes?serviceId=${encodeURIComponent(item.id)}`)
      .then(setPayload)
      .catch((err) => setError(err.message));
  }, [item.id]);
  const nodes = payload?.items || [];
  return (
    <Panel title="接口节点" summary="从服务节点跳转到接口级测试用例模板" className="environment-node-interfaces-panel">
      <div className="environment-node-peer-list">
        {error ? <p className="dashboard-empty">{`接口节点读取失败：${error}`}</p> : null}
        {!payload && !error ? <p className="dashboard-empty">loading</p> : null}
        {payload && !nodes.length ? <p className="dashboard-empty">当前服务还没有登记接口节点。</p> : null}
        {nodes.map((node) => (
          <a className="environment-node-peer" href={node.href} key={node.id}>
            <strong>{node.displayName || node.id}</strong>
            <span>{node.admissionStatus || "pending"}</span>
          </a>
        ))}
      </div>
    </Panel>
  );
}

function MissingNode({ nodeId, snapshot }) {
  const nodes = flattenNodes(snapshot);
  return (
    <section className="environment-node-detail-grid environment-node-missing-grid" aria-label="环境节点详情">
      <Panel title="请求信息" summary="当前 URL 没有匹配到环境快照中的节点。">
        <code className="environment-node-missing-id">{nodeId || "id 参数为空"}</code>
        <DetailList rows={[
          ["requested id", nodeId || "missing query parameter"],
          ["reason", "not found in /api/dashboard groups"],
        ]} />
        <div className="environment-node-detail-actions">
          <a className="button-link" href="/environment-nodes.html">返回环境节点</a>
        </div>
      </Panel>
      <Panel title="可选节点" summary="从当前环境快照选择一个真实存在的服务。">
        <div className="environment-node-candidate-list">
          {nodes.map((candidate) => (
            <a className="environment-node-candidate" href={`/environment-node.html?id=${encodeURIComponent(candidate.id)}`} key={candidate.id}>
              <strong>{candidate.name || candidate.id}</strong>
              <span>{`${candidate.groupLabel || candidate.group || "节点"} · ${statusText(candidate)}`}</span>
            </a>
          ))}
        </div>
      </Panel>
      <SnapshotSummary snapshot={snapshot} />
      <Panel title="快照索引" summary="用于确认当前可恢复节点来自同一次 /api/dashboard 快照。" className="environment-node-missing-snapshot">
        <pre>{JSON.stringify({
          summary: snapshot?.summary || {},
          groups: (snapshot?.groups || []).map((group) => ({
            id: group.id,
            label: group.label,
            count: (group.items || []).length,
            nodes: (group.items || []).map((entry) => entry.id),
          })),
        }, null, 2)}</pre>
      </Panel>
    </section>
  );
}

function NodeDetail({ item, snapshot }) {
  const runtime = runtimeByService(snapshot).get(item.id) || {};
  const nodes = flattenNodes(snapshot);
  return (
    <section className="environment-node-detail-grid" aria-label="环境节点详情">
      <Panel title="运行证据" summary="来自 /api/dashboard 的当前快照" className="environment-node-detail-primary">
        <DetailList rows={[
          ["id", item.id],
          ["container", item.container],
          ["state", item.state],
          ["health", item.health],
          ["message", item.message],
          ["image", item.image],
          ["version", item.version],
        ]} />
      </Panel>
      <Panel title="运行态索引" summary="来自 runtime SQLite 的 service_runtime 表" className="environment-node-runtime-panel">
        <DetailList rows={runtimeRows(item, runtime)} />
      </Panel>
      <Panel title="连接入口" summary="只展示当前快照能证明的端口与页面" className="environment-node-connection-panel">
        <DetailList rows={[
          ["service port", item.port ? `127.0.0.1:${item.port}` : "-"],
          ["management", item.managementPort ? `127.0.0.1:${item.managementPort}` : "-"],
          ["group", item.groupLabel || item.group],
        ]} />
        <ActionLinks item={item} />
      </Panel>
      <InterfaceLinks item={item} />
      <PeerList item={item} nodes={nodes} />
      <SnapshotSummary snapshot={snapshot} />
      <Panel title="原始快照字段" summary="当前节点在 dashboard 快照中的原始字段。" className="environment-node-raw-snapshot">
        <pre>{JSON.stringify(item, null, 2)}</pre>
      </Panel>
    </section>
  );
}

function EnvironmentNodeApp() {
  const [snapshot, setSnapshot] = useState(null);
  const [message, setMessage] = useState("loading");
  const nodeId = requestedId();

  async function refresh() {
    setMessage("refreshing...");
    try {
      setSnapshot(await fetchJSON("/api/dashboard"));
      setMessage("ready");
    } catch (error) {
      setMessage(`failed: ${error.message}`);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const nodes = useMemo(() => flattenNodes(snapshot || {}), [snapshot]);
  const item = nodes.find((candidate) => candidate.id === nodeId);
  const title = item ? item.name || item.id : "未找到环境节点";
  const summary = item ? `${item.groupLabel || item.group || "环境节点"} · ${item.container || item.id}` : nodeId ? `id=${nodeId}` : "缺少 id 参数";
  return (
    <main className="app environment-node-page environment-node-detail-shell" data-template-id="TPL-ENVIRONMENT-NODE-DETAIL-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-ENVIRONMENT-NODE-DETAIL-V1</div>
      <section className="topbar">
        <div>
          <h1>{title}</h1>
          <p>{summary}</p>
        </div>
        <div className="dashboard-top-stats" aria-label="节点状态摘要">
          {item ? (
            <>
              <StatBox label="状态" value={statusText(item)} />
              <StatBox label="端口" value={item.port ? `:${item.port}` : "-"} />
              <StatBox label="Mgmt" value={item.managementPort ? `:${item.managementPort}` : "-"} />
              <StatBox label="分组" value={item.groupLabel || item.group || "-"} />
            </>
          ) : (
            <>
              <StatBox label="节点总数" value={nodes.length} />
              <StatBox label="状态" value="missing" />
            </>
          )}
        </div>
        <div className="actions">
          <span className="environment-status-pill" role="status">{item ? message : "missing"}</span>
          <a className="button-link" href="/environment-nodes.html">环境节点</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <button type="button" title="刷新状态" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>
      {item ? <NodeDetail item={item} snapshot={snapshot || {}} /> : <MissingNode nodeId={nodeId} snapshot={snapshot || {}} />}
    </main>
  );
}

createRoot(document.getElementById("react-environment-node-root")).render(<EnvironmentNodeApp />);
