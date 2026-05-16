import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { DetailList, envCopy, fetchJSON, flattenNodes, runtimeByService, statusText, StatBox } from "./environmentCommon.jsx";

function requestedId() {
  return new URLSearchParams(window.location.search).get("id") || "";
}

function runtimeRows(item, runtime, snapshot) {
  const role = runtime?.nodeRole || item.group || "";
  const showRepo = Boolean(runtime?.branchName || runtime?.commitId) && !["middleware", "platform", "observability", "external"].includes(role);
  if (showRepo) {
    return [
      [envCopy(snapshot, item, "branchLabel", "branch_name"), runtime?.branchName || "-"],
      [envCopy(snapshot, item, "commitLabel", "commit_id"), runtime?.commitId || "-"],
    ];
  }
  return [
    [envCopy(snapshot, item, "imageVersionLabel", "image_version"), runtime?.imageVersion || item.version || "-"],
    [envCopy(snapshot, item, "imageLabel", "image"), runtime?.image || item.image || "-"],
    [envCopy(snapshot, item, "containerStateLabel", "container_state"), runtime?.containerState || item.state || "-"],
    [envCopy(snapshot, item, "healthLabel", "health"), runtime?.health || item.health || "-"],
  ];
}

function compactSourcePath(path) {
  const parts = String(path || "").split("/").filter(Boolean);
  if (parts.length >= 2) {
    return parts.slice(-2).join("/");
  }
  return path || "-";
}

function RuntimeIdentity({ item, runtime, snapshot }) {
  const role = runtime?.nodeRole || item.group || "";
  const showRepo = Boolean(runtime?.branchName || runtime?.commitId) && !["middleware", "platform", "observability", "external"].includes(role);
  if (!showRepo) {
    return <DetailList rows={runtimeRows(item, runtime, snapshot)} />;
  }
  return (
    <dl className="environment-runtime-identity" aria-label={envCopy(snapshot, item, "runtimeIdentityLabel", "运行态代码版本")}>
      <div className="environment-runtime-token">
        <dt>{envCopy(snapshot, item, "branchLabel", "分支")}</dt>
        <dd><code>{runtime.branchName || "-"}</code></dd>
      </div>
      <div className="environment-runtime-token">
        <dt>{envCopy(snapshot, item, "commitLabel", "Commit")}</dt>
        <dd><code>{runtime.commitId || "-"}</code></dd>
      </div>
      {runtime.sourcePath ? (
        <div className="environment-runtime-source">
          <dt>{envCopy(snapshot, item, "sourceSnapshotLabel", "源码快照")}</dt>
          <dd><code title={runtime.sourcePath}>{compactSourcePath(runtime.sourcePath)}</code></dd>
        </div>
      ) : null}
    </dl>
  );
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

function ActionLinks({ item, snapshot }) {
  const links = [
    item.port ? [envCopy(snapshot, item, "openServicePort", "打开服务端口"), `http://127.0.0.1:${item.port}`] : null,
    item.managementPort ? [envCopy(snapshot, item, "openManagementPort", "打开管理端口"), `http://127.0.0.1:${item.managementPort}`] : null,
  ].filter(Boolean);
  return (
    <div className="environment-node-detail-actions">
      {links.length ? (
        links.map(([label, href]) => (
          <a className="button-link" href={href} target="_blank" rel="noreferrer" key={label}>{label}</a>
        ))
      ) : (
        <p className="dashboard-empty">{envCopy(snapshot, item, "noOpenEndpoints", "当前快照没有可打开入口。")}</p>
      )}
    </div>
  );
}

function ConnectionPanel({ item, snapshot }) {
  return (
    <Panel title={envCopy(snapshot, item, "connectionTitle", "连接入口")} summary={envCopy(snapshot, item, "connectionSummary", "当前快照能证明的本机入口")} className="environment-node-connection-panel">
      <div className="environment-endpoint-list">
        <div>
          <span>{envCopy(snapshot, item, "servicePortLabel", "服务端口")}</span>
          <code>{item.port ? `127.0.0.1:${item.port}` : "-"}</code>
        </div>
        <div>
          <span>{envCopy(snapshot, item, "managementPortLabel", "管理端口")}</span>
          <code>{item.managementPort ? `127.0.0.1:${item.managementPort}` : "-"}</code>
        </div>
        <div>
          <span>{envCopy(snapshot, item, "groupLabel", "分组")}</span>
          <code>{item.groupLabel || item.group || "-"}</code>
        </div>
      </div>
      <ActionLinks item={item} snapshot={snapshot} />
    </Panel>
  );
}

function PeerList({ item, nodes, snapshot }) {
  return (
    <Panel title={envCopy(snapshot, item, "peerTitle", "同组节点")} summary={envCopy(snapshot, item, "peerSummary", "同一环境分组里的当前服务状态")} className="environment-node-peers-panel">
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

function SnapshotSummary({ snapshot, item = {} }) {
  const summary = snapshot?.summary || {};
  return (
    <Panel title={envCopy(snapshot, item, "snapshotTitle", "环境快照")} summary={envCopy(snapshot, item, "snapshotSummary", "当前 Control plane 看到的全局健康计数")} className="environment-node-summary-panel">
      <DetailList rows={[
        [envCopy(snapshot, item, "totalLabel", "total"), String(summary.total || 0)],
        [envCopy(snapshot, item, "healthyLabel", "healthy"), String(summary.healthy || 0)],
        [envCopy(snapshot, item, "missingLabel", "missing"), String(summary.missing || 0)],
        [envCopy(snapshot, item, "unhealthyLabel", "unhealthy"), String(summary.unhealthy || 0)],
      ]} />
    </Panel>
  );
}

function InterfaceLinks({ item, snapshot }) {
  const [payload, setPayload] = useState(null);
  const [error, setError] = useState("");
  useEffect(() => {
    fetchJSON(`/api/interface-nodes?serviceId=${encodeURIComponent(item.id)}`)
      .then(setPayload)
      .catch((err) => setError(err.message));
  }, [item.id]);
  const nodes = payload?.items || [];
  return (
    <Panel title={envCopy(snapshot, item, "interfacesTitle", "接口节点")} summary={envCopy(snapshot, item, "interfacesSummary", "从服务节点跳转到接口级测试用例模板")} className="environment-node-interfaces-panel">
      <div className="environment-node-peer-list">
        {error ? <p className="dashboard-empty">{`${envCopy(snapshot, item, "interfaceReadErrorPrefix", "接口节点读取失败")}：${error}`}</p> : null}
        {!payload && !error ? <p className="dashboard-empty">{envCopy(snapshot, item, "loadingLabel", "loading")}</p> : null}
        {payload && !nodes.length ? <p className="dashboard-empty">{envCopy(snapshot, item, "noInterfaceNodes", "当前服务还没有登记接口节点。")}</p> : null}
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
    <section className="environment-node-detail-grid environment-node-missing-grid" aria-label={envCopy(snapshot, {}, "detailGridLabel", "环境节点详情")}>
      <Panel title={envCopy(snapshot, {}, "missingRequestTitle", "请求信息")} summary={envCopy(snapshot, {}, "missingRequestSummary", "当前 URL 没有匹配到环境快照中的节点。")}>
        <code className="environment-node-missing-id">{nodeId || envCopy(snapshot, {}, "emptyIDLabel", "id 参数为空")}</code>
        <DetailList rows={[
          ["requested id", nodeId || "missing query parameter"],
          ["reason", "not found in /api/dashboard groups"],
        ]} />
        <div className="environment-node-detail-actions">
          <a className="button-link" href="/environment-nodes.html">{envCopy(snapshot, {}, "backEnvironmentNodes", "返回环境节点")}</a>
        </div>
      </Panel>
      <Panel title={envCopy(snapshot, {}, "candidateTitle", "可选节点")} summary={envCopy(snapshot, {}, "candidateSummary", "从当前环境快照选择一个真实存在的服务。")}>
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
      <Panel title={envCopy(snapshot, {}, "snapshotIndexTitle", "快照索引")} summary={envCopy(snapshot, {}, "snapshotIndexSummary", "用于确认当前可恢复节点来自同一次 /api/dashboard 快照。")} className="environment-node-missing-snapshot">
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
    <section className="environment-node-detail-grid" aria-label={envCopy(snapshot, item, "detailGridLabel", "环境节点详情")}>
      <Panel title={envCopy(snapshot, item, "detailTitle", "运行证据")} summary={envCopy(snapshot, item, "detailSummary", "来自 /api/dashboard 的当前快照")} className="environment-node-detail-primary">
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
      <Panel title={envCopy(snapshot, item, "runtimeTitle", "运行态索引")} summary={envCopy(snapshot, item, "runtimeSummary", "来自 profile runtime 配置与当前环境快照")} className="environment-node-runtime-panel">
        <RuntimeIdentity item={item} runtime={runtime} snapshot={snapshot} />
      </Panel>
      <ConnectionPanel item={item} snapshot={snapshot} />
      <InterfaceLinks item={item} snapshot={snapshot} />
      <PeerList item={item} nodes={nodes} snapshot={snapshot} />
      <SnapshotSummary snapshot={snapshot} item={item} />
      <Panel title={envCopy(snapshot, item, "rawSnapshotTitle", "原始快照字段")} summary={envCopy(snapshot, item, "rawSnapshotSummary", "当前节点在 dashboard 快照中的原始字段。")} className="environment-node-raw-snapshot">
        <details>
          <summary>{envCopy(snapshot, item, "rawSnapshotToggle", "查看原始快照 JSON")}</summary>
          <pre>{JSON.stringify(item, null, 2)}</pre>
        </details>
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
  const title = item ? item.name || item.id : envCopy(snapshot || {}, {}, "missingEnvironmentTitle", "未找到环境节点");
  const summary = item ? `${item.groupLabel || item.group || envCopy(snapshot || {}, item, "environmentNodeLabel", "环境节点")} · ${item.container || item.id}` : nodeId ? `id=${nodeId}` : envCopy(snapshot || {}, {}, "missingIDSummary", "缺少 id 参数");
  return (
    <main className="app environment-node-page environment-node-detail-shell" data-template-id="TPL-ENVIRONMENT-NODE-DETAIL-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-ENVIRONMENT-NODE-DETAIL-V1</div>
      <section className="topbar">
        <div>
          <h1>{title}</h1>
          <p>{summary}</p>
        </div>
        <div className="dashboard-top-stats" aria-label={envCopy(snapshot || {}, item || {}, "nodeStatusSummaryLabel", "节点状态摘要")}>
          {item ? (
            <>
              <StatBox label={envCopy(snapshot || {}, item, "statusLabel", "状态")} value={statusText(item)} />
              <StatBox label={envCopy(snapshot || {}, item, "portLabel", "端口")} value={item.port || "-"} />
              <StatBox label={envCopy(snapshot || {}, item, "managementStatLabel", "Mgmt")} value={item.managementPort || "-"} />
              <StatBox label={envCopy(snapshot || {}, item, "groupLabel", "分组")} value={item.groupLabel || item.group || "-"} />
            </>
          ) : (
            <>
              <StatBox label={envCopy(snapshot || {}, {}, "nodeCountLabel", "节点总数")} value={nodes.length} />
              <StatBox label={envCopy(snapshot || {}, {}, "statusLabel", "状态")} value="missing" />
            </>
          )}
        </div>
        <div className="actions">
          <span className="environment-status-pill" role="status">{item ? message : "missing"}</span>
          <a className="button-link" href="/environment-nodes.html">{envCopy(snapshot || {}, item || {}, "environmentNodesLink", "环境节点")}</a>
          <a className="button-link" href="/dashboard.html">{envCopy(snapshot || {}, item || {}, "dashboardLink", "环境大盘")}</a>
          <button type="button" title={envCopy(snapshot || {}, item || {}, "refreshTitle", "刷新状态")} onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>{envCopy(snapshot || {}, item || {}, "refreshLabel", "刷新")}</span>
          </button>
        </div>
      </section>
      {item ? <NodeDetail item={item} snapshot={snapshot || {}} /> : <MissingNode nodeId={nodeId} snapshot={snapshot || {}} />}
    </main>
  );
}

createRoot(document.getElementById("react-environment-node-root")).render(<EnvironmentNodeApp />);
