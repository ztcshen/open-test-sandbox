import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { fetchJSON } from "./api.js";

function runIDFromURL() {
  return new URLSearchParams(window.location.search).get("runId") || "";
}

function text(value, fallback = "-") {
  const out = String(value ?? "").trim();
  return out || fallback;
}

function tail(value, length = 12) {
  const out = text(value);
  return out.length <= length ? out : `...${out.slice(-length)}`;
}

function formatTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

function KV({ label, value }) {
  return (
    <article>
      <span>{label}</span>
      <strong>{text(value)}</strong>
    </article>
  );
}

function Fact({ label, value, code = false }) {
  const Body = code ? "code" : "p";
  return (
    <article className="agent-run-detail-item">
      <span>{label}</span>
      <Body>{text(value)}</Body>
    </article>
  );
}

function RelatedRuns({ run, runs }) {
  const related = runs.filter((candidate) => candidate.profileId === run.profileId && candidate.runId !== run.runId).slice(0, 5);
  if (!related.length) return null;
  return (
    <article className="agent-run-detail-item agent-run-related-runs">
      <span>same profile recent runs</span>
      <div className="agent-run-related-list">
        {related.map((candidate) => (
          <a className="agent-run-related-link" href={`/agent-run.html?runId=${encodeURIComponent(candidate.runId || "")}`} key={candidate.runId}>
            {`${candidate.runId || "-"} (${candidate.status || "unknown"}${candidate.failureKind ? ` · ${candidate.failureKind}` : ""})`}
          </a>
        ))}
      </div>
    </article>
  );
}

function Summary({ run, missing, requestedID, runs }) {
  if (missing) {
    return (
      <section className="agent-run-detail-summary" aria-label="Agent run summary">
        <KV label="status" value="missing" />
        <KV label="requested" value={requestedID || "-"} />
        <KV label="recent runs" value={String(runs.length)} />
      </section>
    );
  }
  return (
    <section className="agent-run-detail-summary" aria-label="Agent run summary">
      <KV label="status" value={run.status} />
      <KV label="failureKind" value={run.failureKind || "none"} />
      <KV label="profile" value={run.profileId} />
      <KV label="service" value={run.resolvedServiceId} />
      <KV label="ref" value={run.ref} />
      <KV label="commit" value={run.commitId} />
    </section>
  );
}

function Diagnosis({ run, runs, missing }) {
  if (missing) {
    return (
      <section className="agent-run-detail-panel">
        <div className="agent-test-section-head"><div><h2>Diagnosis</h2><p>恢复入口</p></div></div>
        <div className="agent-run-detail-list">
          <Fact label="next step" value="返回控制台查看运行记录。" />
        </div>
      </section>
    );
  }
  const diagnosis = run.diagnosis || {};
  const blockedReport = run.blockedReport || {};
  const entries = [
    ["reason", diagnosis.reason || run.failureKind || "no failure"],
    ["next step", diagnosis.nextStep || "inspect the Evidence bundle before escalating"],
    ["request id", diagnosis.requestId],
    ["service", diagnosis.service || run.resolvedServiceId],
    ["log excerpt", diagnosis.logExcerpt],
    ["blocked status", blockedReport.status],
    ["blocked reason", blockedReport.reason],
  ].filter(([, value]) => value);
  const related = runs.filter((candidate) => candidate.profileId === run.profileId && candidate.runId !== run.runId).slice(0, 5);
  return (
    <section className="agent-run-detail-panel">
      <div className="agent-test-section-head"><div><h2>Diagnosis</h2><p>{`${entries.length + (related.length ? 1 : 0)} diagnosis fields`}</p></div></div>
      <div className="agent-run-detail-list">
        {entries.map(([label, value]) => <Fact label={label} value={value} key={label} />)}
        <RelatedRuns run={run} runs={runs} />
      </div>
    </section>
  );
}

function Evidence({ run, missing }) {
  if (missing) {
    return (
      <section className="agent-run-detail-panel">
        <div className="agent-test-section-head"><div><h2>Evidence</h2><p>0 evidence pointers</p></div></div>
        <div className="agent-run-detail-list" />
      </section>
    );
  }
  const evidenceRoot = run.evidenceRoot || "";
  const blockedReportPath = evidenceRoot && run.failureKind === "sandbox_capability_gap" ? `${evidenceRoot}/blocked.json` : "";
  const ruleViolations = (run.blockedReport?.rule_violations || [])
    .map((item) => `${item.rule || "-"}: ${item.reason || "-"}`)
    .join("\n");
  const entries = [
    ["evidence root", evidenceRoot],
    ["trace evidence", evidenceRoot ? `${evidenceRoot}/trace-evidence.json` : ""],
    ["blocked report", blockedReportPath],
    ["blocked rules", ruleViolations],
    ["started", formatTime(run.startedAt)],
    ["ended", formatTime(run.endedAt)],
  ].filter(([, value]) => value);
  return (
    <section className="agent-run-detail-panel">
      <div className="agent-test-section-head"><div><h2>Evidence</h2><p>{`${entries.length} evidence pointers`}</p></div></div>
      <div className="agent-run-detail-list">
        {entries.length ? entries.map(([label, value]) => <Fact label={label} value={value} code key={label} />) : <div className="agent-empty">此 Agent run 没有 Evidence bundle 路径。</div>}
      </div>
    </section>
  );
}

function AgentRunApp() {
  const [runs, setRuns] = useState([]);
  const [message, setMessage] = useState("loading");
  const requestedID = runIDFromURL();

  async function refresh() {
    setMessage("refreshing...");
    try {
      const payload = await fetchJSON("/api/agent-test");
      setRuns(payload.agentRuns || []);
      const run = (payload.agentRuns || []).find((candidate) => candidate.runId === requestedID);
      setMessage(run?.status || "missing");
    } catch (error) {
      setRuns([]);
      setMessage(error.message);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const run = runs.find((candidate) => candidate.runId === requestedID) || null;
  const missing = !run;

  return (
    <main className="app agent-run-detail-page">
      <section className="topbar">
        <div>
          <h1>Agent run</h1>
          <p>{missing ? (requestedID ? `未找到 run · ${requestedID}` : "缺少 runId") : run.runId || "-"}</p>
        </div>
        <div className="actions">
          <span className="agent-test-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">控制台</a>
        </div>
      </section>
      <Summary run={run || {}} missing={missing} requestedID={requestedID} runs={runs} />
      <section className="agent-run-detail-shell">
        <Diagnosis run={run || {}} runs={runs} missing={missing} />
        <Evidence run={run || {}} missing={missing} />
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-agent-run-root")).render(<AgentRunApp />);
