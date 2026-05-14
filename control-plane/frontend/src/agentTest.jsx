import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";

const capabilityOrder = [
  "Evidence Diagnosis Index",
  "Config Mutable",
  "Capability Gap",
  "Subagent Acceptance",
];

async function requestJSON(path) {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function blankDash(value) {
  return value === "" || value == null ? "-" : value;
}

function statusTone(status) {
  const value = String(status || "").toLowerCase();
  if (["pass", "passed", "success", "ok"].includes(value)) return "passed";
  if (["fail", "failed", "error"].includes(value)) return "failed";
  return value || "unknown";
}

function capabilitySort(left, right) {
  const leftIndex = capabilityOrder.findIndex((title) => (left.title || "").includes(title));
  const rightIndex = capabilityOrder.findIndex((title) => (right.title || "").includes(title));
  return normalizeOrder(leftIndex) - normalizeOrder(rightIndex);
}

function normalizeOrder(index) {
  return index === -1 ? capabilityOrder.length : index;
}

function countFailureKinds(runs) {
  return runs.reduce((acc, run) => {
    const kind = run.failureKind || "no failure_kind";
    acc[kind] = (acc[kind] || 0) + 1;
    return acc;
  }, {});
}

function Chip({ children }) {
  return <span className="agent-chip">{children}</span>;
}

function Status({ value }) {
  return <span className={`agent-status ${statusTone(value)}`}>{value || "unknown"}</span>;
}

function Empty({ children }) {
  return <div className="agent-empty">{children}</div>;
}

function Panel({ title, summary, action, className = "", children }) {
  return (
    <section className={`agent-test-panel ${className}`}>
      <div className="agent-test-section-head">
        <div>
          <h2>{title}</h2>
          <p>{summary}</p>
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function Ribbon({ summary }) {
  const items = [
    ["profiles", summary.profileCount || 0],
    ["runs", summary.runCount || 0],
    ["config events", summary.configEventCount || 0],
    ["escalations", summary.escalationEventCount || 0],
    ["acceptance", summary.latestAcceptanceVerdict || "-"],
  ];
  return (
    <section className="agent-test-ribbon" aria-label="Agent Test Kit 摘要">
      {items.map(([label, value]) => (
        <div key={label}>
          <span>{label}</span>
          <strong>{value}</strong>
        </div>
      ))}
    </section>
  );
}

function CapabilityGrid({ capabilities }) {
  if (!capabilities.length) {
    return <Empty>未读取到 capability map。</Empty>;
  }
  return (
    <div className="agent-capability-grid">
      {[...capabilities].sort(capabilitySort).map((capability) => (
        <article className="agent-capability-card" key={capability.id || capability.title}>
          <div className="agent-card-top">
            <strong>{capability.title || capability.id || "-"}</strong>
            <Status value={capability.status} />
          </div>
          <p>{capability.description || ""}</p>
          <div className="agent-chip-row">
            {(capability.evidence || []).map((item) => (
              <Chip key={item}>{item}</Chip>
            ))}
          </div>
        </article>
      ))}
    </div>
  );
}

function ProfileList({ profiles }) {
  if (!profiles.length) {
    return <Empty>configs/agent-test-profiles.json 暂无可显示 profile。</Empty>;
  }
  return (
    <div className="agent-profile-list">
      {profiles.map((profile) => (
        <article className="agent-profile-item" key={profile.id}>
          <div className="agent-card-top">
            <strong>{profile.title || profile.id}</strong>
            <code>{profile.id || "-"}</code>
          </div>
          <p>{`${profile.stepCount || 0} steps · ${profile.mysqlProbeCount || 0} probes · ${(profile.allowedChanges || []).length} allowed config changes`}</p>
          <div className="agent-chip-row">
            {(profile.requiredConfig || []).map((cfg) => (
              <Chip key={`${cfg.kind}:${cfg.key}`}>{`${cfg.kind}:${cfg.key}`}</Chip>
            ))}
            {(profile.evidenceKinds || []).map((kind) => (
              <Chip key={kind}>{kind}</Chip>
            ))}
          </div>
        </article>
      ))}
    </div>
  );
}

function RunList({ runs }) {
  if (!runs.length) {
    return <Empty>SQLite 里还没有 agent_runs。</Empty>;
  }
  return (
    <div className="agent-run-list">
      {runs.slice(0, 8).map((run) => (
        <article className={`agent-run-item ${run.status || "unknown"}`} key={run.runId}>
          <div className="agent-card-top">
            <a className="agent-run-link" href={`/agent-run.html?runId=${encodeURIComponent(run.runId || "")}`}>
              {run.runId || "-"}
            </a>
            <Status value={run.status} />
          </div>
          <p>{[run.resolvedServiceId, run.profileId, run.failureKind || "no failure_kind"].filter(Boolean).join(" · ")}</p>
          <p className="agent-diagnosis">{run.diagnosis?.nextStep || run.diagnosis?.reason || run.evidenceRoot || ""}</p>
        </article>
      ))}
    </div>
  );
}

function RunMatrix({ profiles, runs }) {
  const profileIds = profiles.map((profile) => profile.id).filter(Boolean);
  const runProfileIds = [...new Set(runs.map((run) => run.profileId).filter(Boolean))];
  const matrixIds = [...new Set([...profileIds, ...runProfileIds])];
  const profileById = new Map(profiles.map((profile) => [profile.id, profile]));
  if (!matrixIds.length) {
    return <Empty>SQLite 里还没有可交叉查看的 Agent run。</Empty>;
  }
  return (
    <div className="profile-run-matrix">
      {matrixIds.map((profileId) => {
        const profile = profileById.get(profileId) || { id: profileId, title: profileId };
        const profileRuns = runs.filter((run) => run.profileId === profileId);
        const latest = profileRuns[0];
        const passed = profileRuns.filter((run) => run.status === "passed").length;
        const failed = profileRuns.filter((run) => run.status === "failed").length;
        return (
          <article className={`profile-run-card ${latest?.status || "empty"}`} key={profileId}>
            <div className="agent-card-top">
              <strong>{profile.title || profile.id}</strong>
              <Status value={latest?.status || "no run"} />
            </div>
            <p>{`${profileId} · ${profile.stepCount || 0} steps · ${profileRuns.length} runs`}</p>
            <div className="agent-chip-row">
              <Chip>{`${passed} passed`}</Chip>
              <Chip>{`${failed} failed`}</Chip>
              {Object.entries(countFailureKinds(profileRuns))
                .slice(0, 3)
                .map(([kind, count]) => (
                  <Chip key={kind}>{`${kind}: ${count}`}</Chip>
                ))}
            </div>
            <code>{latest?.evidenceRoot || latest?.diagnosis?.nextStep || "no evidence yet"}</code>
            {latest?.runId ? (
              <a className="agent-run-detail-link" href={`/agent-run.html?runId=${encodeURIComponent(latest.runId)}`}>
                查看 run evidence
              </a>
            ) : null}
          </article>
        );
      })}
    </div>
  );
}

function CaseEvidence({ caseRuns }) {
  if (!caseRuns.length) {
    return <Empty>没有 .runtime/cases evidence bundle。</Empty>;
  }
  return (
    <div className="agent-case-evidence-list">
      {caseRuns.slice(0, 6).map((run) => (
        <a className={`agent-case-evidence-item ${statusTone(run.status)}`} href={`/evidence-viewer.html?caseRun=${encodeURIComponent(run.runId || "")}`} key={run.runId}>
          <div className="agent-card-top">
            <strong>{run.caseId || run.runId || "-"}</strong>
            <Status value={run.status} />
          </div>
          <p>{[run.operation, run.failureKind ? `failureKind ${run.failureKind}` : "", run.traceId].filter(Boolean).join(" · ")}</p>
          <code>{run.failureReason || run.evidencePath || "open evidence viewer"}</code>
        </a>
      ))}
    </div>
  );
}

function EventList({ items, emptyText, mapItem }) {
  if (!items.length) {
    return <Empty>{emptyText}</Empty>;
  }
  return (
    <div className="agent-event-list">
      {items.slice(0, 8).map((item) => {
        const view = mapItem(item);
        return (
          <article className="agent-event-item" key={view.key || view.title}>
            <div className="agent-card-top">
              {view.href ? (
                <a className="agent-run-link" href={view.href}>
                  {view.title || "-"}
                </a>
              ) : (
                <strong>{view.title || "-"}</strong>
              )}
              <Status value={view.badge} />
            </div>
            <p>{view.body || ""}</p>
            <code>{view.foot || ""}</code>
          </article>
        );
      })}
    </div>
  );
}

function CapabilityGaps({ events, runs }) {
  const blockedRuns = runs.filter((run) => run.blockedReport);
  const blockedItems = blockedRuns.map((run) => ({
    key: run.runId,
    title: run.runId,
    href: `/agent-run.html?runId=${encodeURIComponent(run.runId || "")}`,
    badge: "Blocked Report",
    body: run.blockedReport?.reason || run.failureKind || "blocked report recorded",
    foot: (run.blockedReport?.rule_violations || []).map((item) => item.rule || item.reason).filter(Boolean).join(" · ") || run.evidenceRoot || "",
  }));
  const escalationItems = events.map((event) => ({
    key: event.eventId || event.runId,
    title: event.eventId || event.runId,
    href: event.runId ? `/agent-run.html?runId=${encodeURIComponent(event.runId)}` : "",
    badge: event.kind || event.status,
    body: event.reason || event.runId || "-",
    foot: event.scope || event.evidenceRoot || "",
  }));
  return (
    <EventList
      items={[...blockedItems, ...escalationItems]}
      emptyText="暂无 blocked report 或 escalation event。"
      mapItem={(item) => item}
    />
  );
}

function AgentTestApp() {
  const [snapshot, setSnapshot] = useState(null);
  const [caseRuns, setCaseRuns] = useState(null);
  const [message, setMessage] = useState("loading");

  async function refresh() {
    setMessage("loading");
    try {
      const [nextSnapshot, nextCaseRuns] = await Promise.all([
        requestJSON("/api/agent-test"),
        requestJSON("/api/case/runs").catch((error) => ({ ok: false, caseRuns: [], warnings: [error.message] })),
      ]);
      setSnapshot(nextSnapshot);
      setCaseRuns(nextCaseRuns);
      const warnings = [...(nextSnapshot.warnings || []), ...(nextCaseRuns.warnings || [])];
      setMessage(warnings.length ? `${warnings.length} warning` : "ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const data = snapshot || {};
  const summary = data.summary || {};
  const profiles = data.profiles || [];
  const runs = data.agentRuns || [];
  const cases = caseRuns?.caseRuns || [];
  const matrixIds = useMemo(() => new Set([...profiles.map((profile) => profile.id), ...runs.map((run) => run.profileId)]), [profiles, runs]);

  return (
    <main className="app agent-test-page">
      <section className="topbar agent-test-topbar">
        <div>
          <h1>Agent Test Kit</h1>
          <p>{`${summary.capabilityCount || 0} capabilities · ${summary.profileCount || 0} profiles · ${summary.runCount || 0} runs`}</p>
        </div>
        <div className="actions">
          <span className="agent-test-status-pill" role="status">
            {message}
          </span>
          <a className="button-link" href="/">
            控制台
          </a>
          <a className="button-link" href="/dashboard.html">
            环境大盘
          </a>
          <button type="button" title="刷新" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>

      <section className="agent-test-shell">
        <Ribbon summary={summary} />
        <Panel title="Profile / Run Matrix" summary={matrixIds.size ? `${matrixIds.size} profiles · ${runs.length} runs` : "暂无 profile/run matrix"}>
          <RunMatrix profiles={profiles} runs={runs} />
        </Panel>
        <Panel
          title="API Case Evidence"
          summary={cases.length ? `${cases.length} case runs · latest ${cases[0].status || "unknown"}` : "暂无 API Case evidence"}
          action={<a className="button-link" href="/case-runs.html">查看全部</a>}
        >
          <CaseEvidence caseRuns={cases} />
        </Panel>
        <Panel title="后端能力" summary={(data.capabilities || []).length ? `${(data.capabilities || []).length} 项后端能力已接入` : "暂无能力定义"}>
          <CapabilityGrid capabilities={data.capabilities || []} />
        </Panel>
        <section className="agent-test-grid">
          <Panel title="Test Profiles" summary={profiles.length ? `${profiles.length} 个 profile` : "暂无 profile"} className="agent-test-profiles-panel">
            <ProfileList profiles={profiles} />
          </Panel>
          <Panel title="最近 Agent Runs" summary={runs.length ? `${runs.length} 条最近记录` : "暂无 run record"}>
            <RunList runs={runs} />
          </Panel>
        </section>
        <section className="agent-test-grid agent-test-lower-grid">
          <Panel title="Config Mutable" summary={(data.configEvents || []).length ? `${(data.configEvents || []).length} 条配置事件` : "暂无配置事件"}>
            <EventList
              items={data.configEvents || []}
              emptyText="暂无记录。"
              mapItem={(event) => ({
                key: event.eventId,
                title: event.eventId,
                badge: event.status || event.kind,
                body: `${event.profileId || "-"} · ${event.kind}:${event.key}`,
                foot: `${blankDash(event.beforeValue)} -> ${blankDash(event.afterValue)}`,
              })}
            />
          </Panel>
          <div className="agent-test-side-stack">
            <Panel
              title="Capability Gap"
              summary={(data.escalationEvents || []).length || runs.filter((run) => run.blockedReport).length ? `${runs.filter((run) => run.blockedReport).length} blocked reports · ${(data.escalationEvents || []).length} escalations` : "暂无 capability gap 留证"}
            >
              <CapabilityGaps events={data.escalationEvents || []} runs={runs} />
            </Panel>
            <Panel title="Subagent Acceptance" summary={(data.acceptanceReports || []).length ? `${(data.acceptanceReports || []).length} 份诊断报告` : "暂无验收报告"}>
              <EventList
                items={data.acceptanceReports || []}
                emptyText="暂无记录。"
                mapItem={(report) => ({
                  key: report.acceptanceId,
                  title: report.acceptanceId,
                  badge: report.verdict || report.status,
                  body: `${(report.caseResults || []).filter((item) => item.status === "passed").length}/${(report.caseResults || []).length} cases passed`,
                  foot: report.reportPath || report.root || "",
                })}
              />
            </Panel>
          </div>
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-agent-test-root")).render(<AgentTestApp />);
