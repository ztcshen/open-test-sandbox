import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { fetchJSON } from "./api.js";

async function requestJSON(path) {
  const payload = await fetchJSON(path);
  if (payload.ok === false) {
    throw new Error(payload.error || "request failed");
  }
  return payload;
}

function statusTone(status) {
  const value = String(status || "").toLowerCase();
  if (["pass", "passed", "success", "ok"].includes(value)) return "passed";
  if (["fail", "failed", "error"].includes(value)) return "failed";
  return "";
}

function shortTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(ms) {
  const value = Number(ms || 0);
  if (!Number.isFinite(value) || value <= 0) return "-";
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)} s`;
}

function formatSpeedup(value) {
  const parsed = Number(value || 0);
  if (!Number.isFinite(parsed) || parsed <= 0) return "-";
  return `${parsed.toFixed(parsed >= 10 ? 0 : 1)}x`;
}

function evidenceHref(run) {
  const params = new URLSearchParams({ caseRun: run.runId || "" });
  if (run.caseId) params.set("caseId", run.caseId);
  return `/evidence-viewer.html?${params.toString()}`;
}

function caseRunSearchText(run) {
  return [run.runId, run.caseId, run.operation, run.traceId, run.status, run.failureKind, run.failureReason, run.evidencePath]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function caseRunDetail(run) {
  return [
    run.operation || "-",
    shortTime(run.updatedAt),
    run.failureKind ? `failureKind ${run.failureKind}` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function timingPath(kind, freshness) {
  const params = new URLSearchParams();
  params.set("kind", kind || "all");
  if (freshness) {
    params.set("maxAgeMinutes", freshness);
  }
  return `/api/case/timing?${params.toString()}`;
}

function timingCommand(kind, freshness) {
  const parts = ["otsandbox", "case", "timing", "--kind", kind || "all"];
  if (freshness) {
    parts.push("--max-age-minutes", freshness);
  }
  return parts.join(" ");
}

function Metric({ label, value }) {
  return (
    <div className="case-timing-metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function CaseRunRow({ run }) {
  return (
    <a className={`run-history-item ${statusTone(run.status)}`} href={evidenceHref(run)}>
      <div className="run-history-top">
        <strong>{run.caseId || run.runId || "-"}</strong>
        <code>{run.status || "-"}</code>
      </div>
      <p>{caseRunDetail(run)}</p>
      <p className="agent-run-detail-note">{run.failureReason || run.traceId || run.evidencePath || "open evidence bundle"}</p>
    </a>
  );
}

function slowestRowText(row) {
  if (!row?.id) return "slowest row: -";
  const caseId = row.caseId ? ` · ${row.caseId}` : "";
  const wallTime = row.wallTimeProxyMs ? ` · wall ${formatDuration(row.wallTimeProxyMs)}` : "";
  return `slowest row: ${row.kind || "-"} · ${row.status || "-"} · ${formatDuration(row.durationMs)} · ${row.id}${caseId}${wallTime}`;
}

function countWarningsByKind(details) {
  return details.reduce((acc, detail) => {
    const detailKind = detail.kind || "unknown";
    acc[detailKind] = (acc[detailKind] || 0) + 1;
    return acc;
  }, {});
}

function TimingSummary({ timing, kind, freshness }) {
  const summary = timing?.summary || {};
  const speedup = summary.speedup || {};
  const slowestRows = summary.slowestRows || {};
  const slowestRow = slowestRows.overall || slowestRows.caseRun || slowestRows.candidateBatch;
  const warnings = timing?.warnings || [];
  const warningCounts = countWarningsByKind(timing?.warningDetails || []);

  return (
    <>
      <div className="case-timing-summary" aria-live="polite">
        {!timing ? (
          <Metric label="timing" value="loading" />
        ) : (
          <>
            <Metric label="case runs" value={summary.caseRunCount || 0} />
            <Metric label="candidate batches" value={summary.candidateBatchCount || 0} />
            <Metric label="measured durations" value={summary.durationMeasuredCount || 0} />
            <Metric label="max duration" value={formatDuration(summary.maxDurationMs)} />
            {speedup.available ? (
              <>
                <Metric label="avg speedup" value={formatSpeedup(speedup.averageEstimatedSpeedup)} />
                <Metric label="max speedup" value={formatSpeedup(speedup.maxEstimatedSpeedup)} />
                <Metric label="wall proxy" value={`${Number(speedup.wallTimeProxyMeasuredCount || 0)} · ${formatDuration(speedup.totalWallTimeProxyMs)}`} />
              </>
            ) : null}
          </>
        )}
      </div>
      <p className="case-timing-slowest">{slowestRowText(slowestRow)}</p>
      <div className="case-timing-slowest-handoff">
        {slowestRow?.id && slowestRow?.source ? (
          <>
            <strong>{`slowest: ${slowestRow.id}`}</strong>
            <code>{slowestRow.source}</code>
          </>
        ) : null}
      </div>
      <div className="case-timing-command" aria-live="polite">
        <code>{timingCommand(kind, freshness)}</code>
        <code>{`${timingCommand(kind, freshness)} --export jsonl`}</code>
        <code>{`${timingCommand(kind, freshness)} --summary-only`}</code>
      </div>
      <div className="case-timing-warning-summary">
        {Object.entries(warningCounts)
          .sort(([left], [right]) => left.localeCompare(right))
          .map(([detailKind, count]) => (
            <span key={detailKind}>{`${detailKind}: ${count}`}</span>
          ))}
      </div>
      <div className="case-timing-warnings">
        {warnings.slice(0, 3).map((warning) => (
          <code key={warning}>{warning}</code>
        ))}
      </div>
    </>
  );
}

function IncompleteBatches({ report }) {
  const items = Array.isArray(report?.items) ? report.items : [];
  const warnings = Array.isArray(report?.warnings) ? report.warnings : [];
  return (
    <>
      <div className="case-incomplete-batch-summary" aria-live="polite">
        <span>{report ? `incomplete batches: ${items.length}` : "incomplete batches: loading"}</span>
        <code>otsandbox case incomplete-batches</code>
      </div>
      <div className="case-incomplete-batch-list">
        {items.length
          ? items.slice(0, 5).map((item) => (
              <div className="case-incomplete-batch-item" key={item.id}>
                <strong>{`${item.id || "-"} · ${item.reason || "unknown"}`}</strong>
                <span>{item.source || item.message || ""}</span>
                <code>{item.suggestedCommand ? `cleanup: ${item.suggestedCommand}` : "cleanup command unavailable"}</code>
              </div>
            ))
          : warnings.slice(0, 2).map((warning) => <code key={warning}>{warning}</code>)}
        {items.length > 5 ? <code>{`+${items.length - 5} more`}</code> : null}
      </div>
    </>
  );
}

function Facets({ caseRuns, visibleRuns, onStatus, onQuery, onReset }) {
  const statusCounts = caseRuns.reduce((acc, run) => {
    const status = run.status || "unknown";
    acc[status] = (acc[status] || 0) + 1;
    return acc;
  }, {});
  const failureKindCounts = caseRuns.reduce((acc, run) => {
    const kind = run.failureKind || "no failureKind";
    acc[kind] = (acc[kind] || 0) + 1;
    return acc;
  }, {});
  return (
    <div className="case-run-facets">
      <button className="agent-chip case-run-facet" type="button" onClick={onReset}>
        {`${visibleRuns.length}/${caseRuns.length} visible`}
      </button>
      {Object.entries(statusCounts).map(([status, count]) => (
        <button className="agent-chip case-run-facet" type="button" key={status} onClick={() => onStatus(status)}>
          {`${status}: ${count}`}
        </button>
      ))}
      {Object.entries(failureKindCounts)
        .slice(0, 4)
        .map(([kind, count]) => (
          <button className="agent-chip case-run-facet" type="button" key={kind} onClick={() => onQuery(kind === "no failureKind" ? "" : kind)}>
            {`failureKind ${kind}: ${count}`}
          </button>
        ))}
    </div>
  );
}

function CaseRunsApp() {
  const [payload, setPayload] = useState(null);
  const [timing, setTiming] = useState(null);
  const [incompleteBatches, setIncompleteBatches] = useState(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [timingKind, setTimingKind] = useState("all");
  const [freshness, setFreshness] = useState("");
  const [message, setMessage] = useState("loading");

  async function loadTiming(kind = timingKind, age = freshness) {
    try {
      return await requestJSON(timingPath(kind, age));
    } catch (error) {
      return { ok: false, summary: {}, warnings: [error.message] };
    }
  }

  async function loadIncompleteBatches() {
    try {
      return await requestJSON("/api/case/incomplete-batches");
    } catch (error) {
      return { ok: false, count: 0, items: [], warnings: [error.message] };
    }
  }

  async function refresh() {
    setMessage("refreshing");
    try {
      const [nextPayload, nextTiming, nextIncompleteBatches] = await Promise.all([
        requestJSON("/api/case/runs"),
        loadTiming(),
        loadIncompleteBatches(),
      ]);
      setPayload(nextPayload);
      setTiming(nextTiming);
      setIncompleteBatches(nextIncompleteBatches);
      setMessage("ready");
    } catch (error) {
      setMessage(error.message);
    }
  }

  async function refreshTiming(kind, age) {
    setMessage("refreshing timing...");
    setTiming(await loadTiming(kind, age));
    setMessage("ready");
  }

  useEffect(() => {
    refresh();
  }, []);

  const caseRuns = payload?.caseRuns || [];
  const visibleRuns = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return caseRuns.filter((run) => {
      const statusOK = !statusFilter || String(run.status || "").toLowerCase() === statusFilter;
      return statusOK && (!normalizedQuery || caseRunSearchText(run).includes(normalizedQuery));
    });
  }, [caseRuns, query, statusFilter]);

  const latest = caseRuns[0];
  const warnings = payload?.warnings || [];
  const summary = latest
    ? `${visibleRuns.length}/${caseRuns.length} case runs · latest ${latest.status || "unknown"} · ${latest.caseId || latest.runId}`
    : "0 case runs";

  return (
    <main className="app case-runs-page">
      <section className="topbar">
        <div>
          <h1>API Case Evidence</h1>
          <p>{summary}</p>
        </div>
        <div className="actions">
          <span className="console-status-pill" role="status" title={warnings.join("\n")}>
            {message}
          </span>
          <a className="button-link" href="/">
            控制台
          </a>
          <a className="button-link" href="/agent-test.html">
            Agent Test Kit
          </a>
          <button type="button" title="刷新" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>

      <section className="agent-test-panel">
        <div className="agent-test-section-head">
          <div>
            <h2>Latest case runs</h2>
            <p>Runtime bundles under .runtime/cases</p>
          </div>
          <div className="case-run-controls">
            <label className="workflow-filter">
              <span>筛选</span>
              <input type="search" placeholder="case / failureKind / trace" spellCheck="false" value={query} onChange={(event) => setQuery(event.target.value)} />
            </label>
            <label className="workflow-filter">
              <span>Timing</span>
              <select
                title="按 timing evidence 类型过滤"
                value={timingKind}
                onChange={(event) => {
                  setTimingKind(event.target.value);
                  refreshTiming(event.target.value, freshness);
                }}
              >
                <option value="all">All timing</option>
                <option value="case">Case runs</option>
                <option value="candidate">Candidate batches</option>
              </select>
            </label>
            <label className="workflow-filter">
              <span>Freshness</span>
              <select
                title="按 timing evidence 新鲜度过滤"
                value={freshness}
                onChange={(event) => {
                  setFreshness(event.target.value);
                  refreshTiming(timingKind, event.target.value);
                }}
              >
                <option value="">All time</option>
                <option value="60">Last 1h</option>
                <option value="360">Last 6h</option>
                <option value="1440">Last 24h</option>
              </select>
            </label>
            <select title="按状态过滤" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
              <option value="">All status</option>
              <option value="fail">Fail</option>
              <option value="pass">Pass</option>
            </select>
          </div>
        </div>
        <Facets
          caseRuns={caseRuns}
          visibleRuns={visibleRuns}
          onStatus={setStatusFilter}
          onQuery={setQuery}
          onReset={() => {
            setQuery("");
            setStatusFilter("");
          }}
        />
        <TimingSummary timing={timing} kind={timingKind} freshness={freshness} />
        <IncompleteBatches report={incompleteBatches} />
        <div className="case-run-list run-history-grid">
          {visibleRuns.length ? (
            visibleRuns.slice(0, 24).map((run) => <CaseRunRow run={run} key={run.runId || `${run.caseId}-${run.updatedAt}`} />)
          ) : (
            <div className="run-history-empty">{caseRuns.length ? "没有匹配的 API Case evidence" : warnings[0] || "暂无 API Case evidence"}</div>
          )}
        </div>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-case-runs-root")).render(<CaseRunsApp />);
