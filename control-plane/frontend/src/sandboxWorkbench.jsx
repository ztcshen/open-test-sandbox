import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { fetchJSON } from "./api.js";

function text(value, fallback = "-") {
  const out = String(value ?? "").trim();
  return out || fallback;
}

function shortTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { hour12: false });
}

function parseSummary(raw) {
  if (!raw) return {};
  try {
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

function runStatusTone(status) {
  const value = String(status || "").toLowerCase();
  if (["pass", "passed", "success", "ok"].includes(value)) return "passed";
  if (["fail", "failed", "error"].includes(value)) return "failed";
  if (["blocked", "warning"].includes(value)) return "warning";
  return value;
}

function workflowServiceHref(service) {
  if (service?.role === "external") {
    return `/service-inventory.html#service-${encodeURIComponent(service.id || "external")}`;
  }
  return `/environment-node.html?id=${encodeURIComponent(service?.id || "")}`;
}

function latestFailedWorkflowRun(runs) {
  return (runs || []).find((run) => run.status === "failed");
}

async function postJSON(path, payload) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "content-type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload || {}),
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    const error = new Error(body.error || response.statusText);
    error.payload = body;
    throw error;
  }
  return body;
}

function CapabilityCard({ card }) {
  return (
    <a className="sandbox-capability-card" href={card.href}>
      <span>{card.meta}</span>
      <strong>{card.title}</strong>
      <p>{card.detail}</p>
    </a>
  );
}

function CapabilityGrid({ runs, caseRuns, catalog }) {
  const workflowRuns = runs?.workflowRuns || [];
  const latestRun = workflowRuns[0];
  const latestCaseRun = caseRuns?.caseRuns?.[0];
  const cards = [
    {
      title: "Workflow Evidence",
      detail: "Requests, responses, logs, journal entries, database hints, and trace topology.",
      href: latestRun ? `/workflow-run.html?id=${encodeURIComponent(latestRun.id)}` : "/workflow-run.html",
      meta: latestRun ? `latest ${latestRun.status}` : "no run yet",
    },
    {
      title: "Run Topology",
      detail: "Confirmed edges, external exits, unresolved exits, request ids, and trace ids.",
      href: latestRun ? `/trace-topology.html?workflowRunId=${encodeURIComponent(latestRun.id)}` : "/trace-topology.html",
      meta: latestRun ? `run #${latestRun.id}` : "no run yet",
    },
    {
      title: "API Case Evidence",
      detail: "Runtime case bundles, request and response snapshots, trace continuity, and failure kind.",
      href: "/case-runs.html",
      meta: latestCaseRun ? `${latestCaseRun.status || "unknown"} · ${latestCaseRun.failureKind || "no failure kind"}` : "no case run yet",
    },
    {
      title: "Service Inventory",
      detail: "Registry-backed services, runtime nodes, containers, ports, and declared dependencies.",
      href: "/service-inventory.html",
      meta: `${catalog?.services?.length || 0} services`,
    },
    {
      title: "Replay And Probe",
      detail: "Replay fixtures, negative probes, capability evidence, and persisted reports.",
      href: "/workflow-detail.html?id=sandbox.replay_probe_observability",
      meta: `${runs?.probeRuns?.length || 0} probes`,
    },
  ];
  return <section className="sandbox-capability-grid home-capability-grid-density" aria-label="Sandbox 能力">{cards.map((card) => <CapabilityCard card={card} key={card.title} />)}</section>;
}

function RunItem({ title, meta, detail, tone = "", href = "" }) {
  const Body = href ? "a" : "article";
  return (
    <Body className={`run-history-item ${tone}`} href={href || undefined}>
      <div className="run-history-top">
        <strong>{title || "-"}</strong>
        <code>{meta || "-"}</code>
      </div>
      <p>{detail || "-"}</p>
    </Body>
  );
}

function RunGroup({ title, rows, renderRow }) {
  return (
    <section className="run-history-group">
      <h3>{title}</h3>
      {rows?.length ? rows.slice(0, 6).map(renderRow) : <div className="run-history-empty">暂无记录</div>}
    </section>
  );
}

function RunHistory({ runs, caseRuns, onRefresh }) {
  return (
    <section className="run-history">
      <div className="section-head">
        <h2>运行历史</h2>
        <button type="button" title="刷新运行历史" onClick={onRefresh}><RefreshCw size={15} aria-hidden="true" /></button>
      </div>
      <div className="run-history-grid">
        <RunGroup
          title="Workflow runs"
          rows={runs?.workflowRuns || []}
          renderRow={(row) => {
            const summary = parseSummary(row.summaryJson);
            const stepCount = row.stepCount || summary.summary?.stepCount || summary.steps?.length || "-";
            return <RunItem title={row.workflowId} meta={row.status} detail={`${shortTime(row.createdAt)} · steps ${stepCount}`} tone={row.status} href={`/workflow-run.html?id=${encodeURIComponent(row.id)}`} key={`workflow-${row.id}`} />;
          }}
        />
        <RunGroup
          title="Replay runs"
          rows={runs?.replayRuns || []}
          renderRow={(row) => <RunItem title={row.traceId} meta={`${row.httpStatus || "-"} HTTP`} detail={`${shortTime(row.createdAt)} · ${row.targetUrl || "-"}`} href={row.traceId ? `/replay-evidence.html?traceId=${encodeURIComponent(row.traceId)}` : ""} key={`replay-${row.traceId}`} />}
        />
        <RunGroup
          title="API case runs"
          rows={caseRuns?.caseRuns || []}
          renderRow={(row) => <RunItem title={row.caseId || row.runId} meta={row.status || "-"} detail={`${shortTime(row.updatedAt)} · ${row.failureKind || row.operation || "-"}`} tone={runStatusTone(row.status)} href={row.runId ? `/evidence-viewer.html?${new URLSearchParams({ caseRun: row.runId, caseId: row.caseId || "" }).toString()}` : ""} key={`case-${row.runId}`} />}
        />
        <RunGroup
          title="Probe reports"
          rows={runs?.probeRuns || []}
          renderRow={(row) => <RunItem title={row.service || "probe"} meta={row.detected ? "detected" : "not detected"} detail={`${shortTime(row.createdAt)} · ${row.traceId || "-"}`} tone={row.detected ? "passed" : ""} key={`probe-${row.traceId || row.service}`} />}
        />
      </div>
    </section>
  );
}

function Topology({ catalog }) {
  const services = catalog?.services || [];
  const serviceByID = new Map(services.map((service) => [service.id, service]));
  const edges = catalog?.topology?.edges || [];
  return (
    <section className="services">
      <div className="section-head">
        <h2>服务拓扑</h2>
        <a className="button-link" href="/service-inventory.html">查看全部</a>
      </div>
      <div className="sandbox-topology-list">
        {edges.length ? edges.map((edge) => {
          const from = serviceByID.get(edge.from)?.displayName || edge.from;
          const to = serviceByID.get(edge.to)?.displayName || edge.to;
          return (
            <a className="sandbox-topology-edge" href={workflowServiceHref(serviceByID.get(edge.from) || { id: edge.from })} key={`${edge.from}-${edge.to}`}>
              <strong>{from}</strong>
              <span>{"->"}</span>
              <strong>{to}</strong>
            </a>
          );
        }) : <div className="run-history-empty">Catalog 未声明拓扑边</div>}
      </div>
    </section>
  );
}

function EvidenceLinks({ runs }) {
  const workflowRuns = runs?.workflowRuns || [];
  const latestRun = workflowRuns[0];
  const failedRun = latestFailedWorkflowRun(workflowRuns);
  const links = [
    [latestRun ? `Latest Workflow Run #${latestRun.id}` : "Latest Workflow Run", latestRun ? `/workflow-run.html?id=${encodeURIComponent(latestRun.id)}` : "/workflow-run.html"],
    [failedRun ? `Latest Failed Workflow #${failedRun.id}` : "Latest Failed Workflow", failedRun ? `/workflow-run.html?id=${encodeURIComponent(failedRun.id)}` : "/workflow-run.html"],
    [latestRun ? `Latest Run Topology #${latestRun.id}` : "Latest Run Topology", latestRun ? `/trace-topology.html?workflowRunId=${encodeURIComponent(latestRun.id)}` : "/trace-topology.html"],
    [failedRun ? `Latest Failed Topology #${failedRun.id}` : "Latest Failed Topology", failedRun ? `/trace-topology.html?workflowRunId=${encodeURIComponent(failedRun.id)}&exitKind=unresolved` : "/trace-topology.html"],
    ["Workflow 目录", "/workflows.html"],
    ["接口节点目录", "/interface-nodes.html"],
    ["API Case Evidence", "/case-runs.html"],
    ["Replay / Capability probe", "/workflow-detail.html?id=sandbox.replay_probe_observability"],
    ["Effective config", "/workflow-detail.html?id=sandbox.platform_config_check"],
  ];
  return (
    <section className="services">
      <div className="section-head"><h2>证据入口</h2></div>
      <div className="sandbox-link-list">
        {links.map(([label, href]) => <a href={href} key={label}>{label}</a>)}
      </div>
    </section>
  );
}

function serviceHealthTone(service) {
  if (service.error || service.exists === false) return "failed";
  if (service.dirty || (service.desiredBranch && service.currentBranch && service.desiredBranch !== service.currentBranch)) return "warning";
  return "passed";
}

function serviceHealthLabel(service) {
  if (service.error) return "error";
  if (service.exists === false) return "external";
  if (service.dirty) return "dirty";
  if (service.desiredBranch && service.currentBranch && service.desiredBranch !== service.currentBranch) return "branch drift";
  return "clean";
}

function ServiceHealth({ snapshot }) {
  const services = snapshot?.services || [];
  return (
    <section className="services">
      <div className="section-head">
        <h2>Service Health</h2>
        <a className="button-link" href="/dashboard.html">环境大盘</a>
      </div>
      <div className="home-service-health-list">
        {services.length ? services.slice(0, 8).map((service) => (
          <a className={`home-service-health-item ${serviceHealthTone(service)}`} href={service.exists === false ? `/service-inventory.html#service-${encodeURIComponent(service.id || "")}` : `/environment-node.html?id=${encodeURIComponent(service.id || "")}`} key={service.id}>
            <div className="run-history-top">
              <strong>{service.name || service.id || "-"}</strong>
              <code>{serviceHealthLabel(service)}</code>
            </div>
            <p>{[service.currentBranch || service.kind || "-", service.currentCommit || service.targetCommit || "-", service.status || service.error || ""].filter(Boolean).join(" · ")}</p>
          </a>
        )) : <div className="run-history-empty">暂无 service health</div>}
      </div>
    </section>
  );
}

function ProfileImportPanel({ onImported }) {
  const [path, setPath] = useState("/path/to/profile-bundle");
  const [audit, setAudit] = useState(true);
  const [requireAuditOk, setRequireAuditOk] = useState(false);
  const [requireCaseRuns, setRequireCaseRuns] = useState(false);
  const [requireWorkflowRuns, setRequireWorkflowRuns] = useState(false);
  const [installForce, setInstallForce] = useState(false);
  const [message, setMessage] = useState("ready");
  const [report, setReport] = useState(null);
  const [installedProfiles, setInstalledProfiles] = useState([]);
  const [profileHome, setProfileHome] = useState("");

  async function loadInstalledProfiles() {
    try {
      const payload = await fetchJSON("/api/profile/installed");
      setInstalledProfiles(payload.profiles || []);
      setProfileHome(payload.profileHome || "");
    } catch (error) {
      setInstalledProfiles([]);
      setProfileHome(error.message);
    }
  }

  async function runImport() {
    setMessage("importing...");
    setReport(null);
    try {
      const nextReport = await postJSON("/api/profile/import", { path, audit, requireAuditOk, force: installForce });
      setReport(nextReport);
      setMessage("imported");
      onImported?.();
    } catch (error) {
      setMessage(error.message);
    }
  }

  async function runInstall() {
    setMessage("installing...");
    setReport(null);
    try {
      const installReport = await postJSON("/api/profile/install", { path, force: installForce });
      setPath(installReport.id || path);
      setReport({ profileId: installReport.id, bundleDigest: installReport.bundleDigest, counts: {} });
      setMessage("installed");
      await loadInstalledProfiles();
    } catch (error) {
      setMessage(error.message);
    }
  }

  async function runVerify() {
    setMessage("verifying...");
    setReport(null);
    try {
      const nextReport = await postJSON("/api/profile/verify", { path, requireCaseRuns, requireWorkflowRuns, force: installForce });
      setReport(nextReport);
      setMessage(nextReport.ok ? "verified" : "verification failed");
      onImported?.();
    } catch (error) {
      if (error.payload?.checks || error.payload?.summary) {
        setReport(error.payload);
      }
      setMessage(error.message);
    }
  }

  async function submit(event) {
    event.preventDefault();
    await runImport();
  }

  useEffect(() => {
    loadInstalledProfiles();
  }, []);

  const reportProfileId = report?.profileId || report?.publish?.profileId || "";
  const reportCounts = report?.counts || report?.publish?.counts || {};
  const reportAudit = report?.audit;
  const reportVersion = report?.configVersion?.id || report?.publish?.configVersion?.id || "";
  const reportChecks = report?.checks || [];
  const reportSummary = report?.summary || null;
  const passedChecks = reportSummary?.passedChecks ?? reportChecks.filter((item) => item.ok).length;
  const totalChecks = reportSummary?.totalChecks ?? reportChecks.length;
  const failedChecks = reportSummary?.failedChecks ?? Math.max(totalChecks - passedChecks, 0);
  const selectedInstalledProfile = installedProfiles.find((item) => item.id === path)?.id || "";

  return (
    <section className="services">
      <div className="section-head">
        <h2>Profile 导入</h2>
        <span className="console-status-pill" role="status">{message}</span>
      </div>
      <form className="sandbox-link-list" onSubmit={submit}>
        <label className="workflow-filter">
          <span>路径 / ID</span>
          <input type="text" value={path} onChange={(event) => setPath(event.target.value)} spellCheck="false" />
        </label>
        <label className="workflow-filter">
          <span>已安装</span>
          <select value={selectedInstalledProfile} onChange={(event) => event.target.value && setPath(event.target.value)} title={profileHome || "Profile home"}>
            <option value="">选择 Profile</option>
            {installedProfiles.map((item) => (
              <option value={item.id} key={item.id} disabled={item.valid === false}>
                {item.valid === false ? `${item.id} · invalid` : `${item.id} · ${item.counts?.workflows || 0} workflows`}
              </option>
            ))}
          </select>
        </label>
        <label className="check-row compact-check">
          <input type="checkbox" checked={audit} onChange={(event) => setAudit(event.target.checked)} />
          <span>导入后审计</span>
        </label>
        <label className="check-row compact-check">
          <input type="checkbox" checked={requireAuditOk} onChange={(event) => setRequireAuditOk(event.target.checked)} />
          <span>审计不通过则阻断</span>
        </label>
        <label className="check-row compact-check">
          <input type="checkbox" checked={requireCaseRuns} onChange={(event) => setRequireCaseRuns(event.target.checked)} />
          <span>要求用例已通过</span>
        </label>
        <label className="check-row compact-check">
          <input type="checkbox" checked={requireWorkflowRuns} onChange={(event) => setRequireWorkflowRuns(event.target.checked)} />
          <span>要求工作流已通过</span>
        </label>
        <label className="check-row compact-check">
          <input type="checkbox" checked={installForce} onChange={(event) => setInstallForce(event.target.checked)} />
          <span>覆盖安装</span>
        </label>
        <button className="button-link" type="button" onClick={runInstall}>安装到本地</button>
        <button className="button-link primary-link" type="submit">一键导入</button>
        <button className="button-link" type="button" onClick={runVerify}>验收并发布</button>
      </form>
      {report ? (
        <div className="profile-verify-report">
          <div className="profile-verify-summary">
            {`${reportProfileId} · ${reportCounts.apiCases || 0} cases · ${reportCounts.workflows || 0} workflows${reportAudit ? ` · issues ${reportAudit.issueCount || 0}` : ""}${totalChecks ? ` · checks ${passedChecks}/${totalChecks}` : ""}${reportVersion ? ` · ${reportVersion}` : ""}`}
          </div>
          {reportSummary ? (
            <div className="profile-verify-metrics">
              <span className={failedChecks ? "failed" : "passed"}>{failedChecks ? `${failedChecks} failed` : "all passed"}</span>
              <span>{`case runs ${reportSummary.requiredCaseRuns ? "required" : "optional"}`}</span>
              <span>{`workflow runs ${reportSummary.requiredWorkflowRuns ? "required" : "optional"}`}</span>
              {reportSummary.firstFailed ? <span>{`first failed ${reportSummary.firstFailed}`}</span> : null}
            </div>
          ) : null}
          {reportChecks.length ? (
            <div className="profile-check-list" aria-label="Profile verification checks">
              {reportChecks.slice(0, 12).map((item) => (
                <div className={item.ok ? "profile-check-row passed" : "profile-check-row failed"} key={item.name}>
                  <strong>{item.name}</strong>
                  <span>{item.ok ? "passed" : "failed"}</span>
                  <p>{item.detail}</p>
                </div>
              ))}
              {reportChecks.length > 12 ? <p className="profile-check-overflow">{`还有 ${reportChecks.length - 12} 项检查未展开`}</p> : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

function SandboxWorkbenchApp() {
  const [snapshot, setSnapshot] = useState(null);
  const [catalog, setCatalog] = useState(null);
  const [runs, setRuns] = useState(null);
  const [caseRuns, setCaseRuns] = useState(null);
  const [message, setMessage] = useState("loading");

  async function refresh() {
    setMessage("refreshing...");
    try {
      const [nextSnapshot, nextCatalog, nextRuns, nextCaseRuns] = await Promise.all([
        fetchJSON("/api/state"),
        fetchJSON("/api/catalog"),
        fetchJSON("/api/runs"),
        fetchJSON("/api/case/runs").catch((error) => ({ ok: false, caseRuns: [], warnings: [error.message] })),
      ]);
      setSnapshot(nextSnapshot);
      setCatalog(nextCatalog);
      setRuns(nextRuns);
      setCaseRuns(nextCaseRuns);
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  async function refreshRuns() {
    try {
      const [nextRuns, nextCaseRuns] = await Promise.all([
        fetchJSON("/api/runs"),
        fetchJSON("/api/case/runs").catch((error) => ({ ok: false, caseRuns: [], warnings: [error.message] })),
      ]);
      setRuns(nextRuns);
      setCaseRuns(nextCaseRuns);
      setMessage("run history refreshed");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const services = catalog?.services || [];
  const workflows = catalog?.workflows || [];
  const workflowRuns = runs?.workflowRuns || [];
  const apiCaseRuns = caseRuns?.caseRuns || [];
  const summary = `${services.length} services · ${workflows.length} workflows · ${workflowRuns.length} workflow runs · ${apiCaseRuns.length} case runs`;

  return (
    <main className="app console-page sandbox-workbench-page">
      <section className="topbar">
        <div>
          <h1>Sandbox 测试工作台</h1>
          <p>{summary}</p>
        </div>
        <div className="actions">
          <span className="console-status-pill" role="status">{message}</span>
          <a className="button-link primary-link" href="/workflows.html">Workflow 目录</a>
          <a className="button-link primary-link" href="/interface-nodes.html">接口节点</a>
          <a className="button-link" href="/service-inventory.html">服务目录</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <button type="button" title="刷新" onClick={refresh}><RefreshCw size={15} aria-hidden="true" /></button>
        </div>
      </section>
      <CapabilityGrid runs={runs} caseRuns={caseRuns} catalog={catalog} />
      <section className="sandbox-workbench-layout">
        <div className="main-column">
          <RunHistory runs={runs} caseRuns={caseRuns} onRefresh={refreshRuns} />
        </div>
        <aside className="sandbox-workbench-side">
          <ProfileImportPanel onImported={refresh} />
          <Topology catalog={catalog} />
          <EvidenceLinks runs={runs} />
          <ServiceHealth snapshot={snapshot} />
        </aside>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-sandbox-workbench-root")).render(<SandboxWorkbenchApp />);
