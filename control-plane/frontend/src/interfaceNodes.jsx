import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { fetchJSON } from "./api.js";

function text(value, fallback = "-") {
  const out = String(value ?? "").trim();
  return out || fallback;
}

function copyText(payload, key, fallback) {
  return payload?.presentation?.copy?.[key] || fallback;
}

function duration(ms) {
  const value = Number(ms || 0);
  if (!Number.isFinite(value) || value <= 0) return "-";
  if (value < 1000) return `${Math.round(value)}ms`;
  return `${(value / 1000).toFixed(1)}s`;
}

function countsFor(items) {
  return items.reduce((counts, item) => {
    counts.total += 1;
    const admission = item.admissionStatus || "pending";
    counts[admission] = (counts[admission] || 0) + 1;
    if (item.validationStatus === "invalid") counts.invalid += 1;
    if (item.serviceId) counts.services.add(item.serviceId);
    return counts;
  }, { total: 0, passed: 0, failed: 0, pending: 0, invalid: 0, services: new Set() });
}

function statusClass(item) {
  if (item.admissionStatus === "passed") return "good";
  if (item.admissionStatus === "failed") return "bad";
  return "warn";
}

function Stats({ items, payload }) {
  const counts = countsFor(items);
  const rows = [
    [copyText(payload, "nodesStat", "节点"), counts.total],
    [copyText(payload, "servicesStat", "服务"), counts.services.size],
    [copyText(payload, "passedStat", "通过"), counts.passed || 0],
    [copyText(payload, "attentionStat", "待处理"), (counts.failed || 0) + (counts.pending || 0) + counts.invalid],
  ];
  return (
    <div className="interface-node-directory-summary">
      {rows.map(([label, value]) => (
        <div className="interface-node-directory-summary-card" key={label}>
          <strong>{value}</strong>
          <span>{label}</span>
        </div>
      ))}
    </div>
  );
}

function Attention({ items, payload }) {
  const attention = items
    .filter((item) => item.admissionStatus !== "passed" || item.validationStatus === "invalid")
    .slice(0, 8);
  return (
    <div className="interface-node-directory-attention-list">
      {attention.length ? attention.map((item) => (
        <a className="interface-node-directory-attention-item" href={item.href || `/interface-node.html?id=${encodeURIComponent(item.id || "")}`} key={item.id}>
          <strong>{item.displayName || item.id || copyText(payload, "fallbackNodeName", "接口节点")}</strong>
          <span>{[item.admissionStatus || "pending", item.validationStatus === "invalid" ? `${item.validationIssueCount ?? 0} validation` : "", item.serviceId].filter(Boolean).join(" · ")}</span>
        </a>
      )) : <p className="dashboard-empty compact">{copyText(payload, "attentionEmpty", "当前没有待处理接口。")}</p>}
    </div>
  );
}

function NodeCard({ item, payload }) {
  return (
    <a className="interface-node-directory-card" href={item.href || `/interface-node.html?id=${encodeURIComponent(item.id || "")}`}>
      <div className="interface-node-directory-card-top">
        <strong>{item.displayName || item.id || copyText(payload, "fallbackNodeName", "接口节点")}</strong>
        <span className={`react-pill ${statusClass(item)}`}>{item.admissionStatus || "pending"}</span>
      </div>
      <code>{[item.id, item.serviceId, item.operation].filter(Boolean).join(" · ") || "-"}</code>
      <p>{`${text(item.method)} ${text(item.path)}`}</p>
      <div className="interface-node-directory-card-details">
        <span>{`${item.passedCaseCount ?? 0}/${item.requiredCaseCount ?? 0} ${copyText(payload, "requiredCasesLabel", "required cases")}`}</span>
        <span>{`${copyText(payload, "latestElapsedLabel", "最近耗时")} ${duration(item.latestElapsedMs)}`}</span>
        <span>{`${copyText(payload, "totalElapsedLabel", "总耗时")} ${duration(item.totalElapsedMs)}`}</span>
        <span>{`${copyText(payload, "timeoutLabel", "最大超时")} ${duration(item.timeoutMs)}`}</span>
        <span>{item.validationStatus === "invalid" ? `${copyText(payload, "validationIssueLabel", "validation issues")} ${item.validationIssueCount ?? 0}` : copyText(payload, "validationOkLabel", "validation ok")}</span>
      </div>
    </a>
  );
}

function InterfaceNodesApp() {
  const [payload, setPayload] = useState({ items: [], source: {} });
  const [query, setQuery] = useState("");
  const [serviceID, setServiceID] = useState("");
  const [message, setMessage] = useState("loading");

  async function refresh() {
    setMessage("refreshing...");
    try {
      setPayload(await fetchJSON("/api/interface-nodes"));
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const items = payload.items || [];
  const services = useMemo(() => [...new Set(items.map((item) => item.serviceId).filter(Boolean))].sort(), [items]);
  const visible = items.filter((item) => {
    if (serviceID && item.serviceId !== serviceID) return false;
    if (!query.trim()) return true;
    const haystack = [
      item.id,
      item.displayName,
      item.serviceId,
      item.operation,
      item.method,
      item.path,
      item.status,
      item.admissionStatus,
      item.validationStatus,
    ].filter(Boolean).join(" ").toLowerCase();
    return haystack.includes(query.trim().toLowerCase());
  });
  const source = payload.source || {};

  return (
    <main className="app interface-node-directory-page" data-template-id="TPL-INTERFACE-NODE-DIRECTORY-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-INTERFACE-NODE-DIRECTORY-V1</div>
      <section className="topbar">
        <div>
          <h1>{copyText(payload, "directoryTitle", "接口节点目录")}</h1>
          <p>{`${visible.length}/${items.length} ${copyText(payload, "directoryCountSuffix", "interface nodes")}`}</p>
        </div>
        <div className="actions">
          <span className="dashboard-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">{copyText(payload, "consoleLink", "控制台")}</a>
          <a className="button-link" href="/workflows.html">{copyText(payload, "workflowDirectoryLink", "Workflow 目录")}</a>
          <a className="button-link" href="/service-inventory.html">{copyText(payload, "serviceDirectoryLink", "服务目录")}</a>
          <button type="button" title={copyText(payload, "refreshTitle", "刷新")} onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
          </button>
        </div>
      </section>

      <section className="interface-node-directory" aria-label={copyText(payload, "directoryAriaLabel", "接口节点目录工作台")}>
        <div className="interface-node-directory-main">
          <div className="dashboard-section-head interface-node-directory-head">
            <div>
              <h2>{copyText(payload, "directoryPanelTitle", "Interface Nodes")}</h2>
              <p>{`${source.kind || "profile"}${source.path ? ` · ${source.path}` : ""}`}</p>
            </div>
            <div className="interface-node-directory-filters">
              <label>
                <span>{copyText(payload, "searchLabel", "搜索")}</span>
                <input type="search" placeholder={copyText(payload, "searchPlaceholder", "接口 / 服务 / operation")} spellCheck="false" value={query} onChange={(event) => setQuery(event.target.value)} />
              </label>
              <label>
                <span>{copyText(payload, "serviceFilterLabel", "服务")}</span>
                <select value={serviceID} onChange={(event) => setServiceID(event.target.value)}>
                  <option value="">{copyText(payload, "allServicesOption", "全部服务")}</option>
                  {services.map((id) => <option value={id} key={id}>{id}</option>)}
                </select>
              </label>
            </div>
          </div>
          <div className="interface-node-directory-list">
            {visible.length ? visible.map((item) => <NodeCard item={item} payload={payload} key={item.id} />) : <p className="dashboard-empty">{copyText(payload, "emptyFilterResult", "没有匹配的接口节点。")}</p>}
          </div>
        </div>
        <aside className="interface-node-directory-side" aria-label={copyText(payload, "directorySummaryAriaLabel", "接口节点汇总")}>
          <Stats items={items} payload={payload} />
          <div className="interface-node-directory-attention">
            <div className="dashboard-section-head compact">
              <div>
                <h2>{copyText(payload, "attentionTitle", "Attention")}</h2>
                <p>{copyText(payload, "attentionSubtitle", "failed / pending admission")}</p>
              </div>
            </div>
            <Attention items={items} payload={payload} />
          </div>
        </aside>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-interface-nodes-root")).render(<InterfaceNodesApp />);
