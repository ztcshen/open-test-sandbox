import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { ArrowDownUp, RefreshCw } from "lucide-react";
import { fetchJSON } from "./api.js";
import { buildRunAnalysis } from "./caseRunsModel.mjs";

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

function evidenceHref(run, workflowId = "") {
  const params = new URLSearchParams({ caseRun: run.runId || "" });
  if (run.caseId) params.set("caseId", run.caseId);
  if (workflowId) params.set("workflow", workflowId);
  return `/evidence-viewer.html?${params.toString()}`;
}

function caseRunDetail(run) {
  return [
    run.operation || "-",
    shortTime(run.updatedAt),
    run.failureCategory ? `category ${run.failureCategory}` : run.failureKind ? `failureKind ${run.failureKind}` : "",
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
  const parts = ["agent-testbench", "case", "timing", "--kind", kind || "all"];
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

function CaseRunGridRow({ row }) {
  return (
    <tr className={`case-run-report-row ${statusTone(row.status)}`}>
      <td><span className={`agent-status ${statusTone(row.status)}`}>{row.status || "-"}</span></td>
      <td>
        <a className="case-run-case-link" href={row.evidenceHref}>{row.caseId || row.id}</a>
        {row.traceId ? <code>{row.traceId}</code> : null}
      </td>
      <td>
        <strong>{row.operation || "-"}</strong>
        <span>{row.failureReason || "runtime bundle available"}</span>
      </td>
      <td><button className="case-run-category-button" type="button" data-category={row.failureCategory}>{row.failureCategory || "-"}</button></td>
      <td className="numeric"><strong>{formatDuration(row.durationMs)}</strong><span>{`#${row.durationRank}`}</span></td>
      <td><span>{shortTime(row.updatedAt)}</span></td>
      <td><a className="button-link case-run-evidence-action" href={row.evidenceHref}>Evidence</a></td>
    </tr>
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
        <code>agent-testbench case incomplete-batches</code>
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

function Facets({ analysis, onStatus, onFailureCategory, onReset }) {
  return (
    <div className="case-run-facets">
      <button className="agent-chip case-run-facet" type="button" onClick={onReset}>
        {`${analysis.summary.visible}/${analysis.summary.total} visible`}
      </button>
      {analysis.statusFacets.map((facet) => (
        <button className="agent-chip case-run-facet" type="button" key={`status-${facet.key}`} onClick={() => onStatus(facet.key)}>
          {`${facet.label}: ${facet.count}`}
        </button>
      ))}
      {analysis.failureCategoryFacets.slice(0, 6).map((facet) => (
        <button className="agent-chip case-run-facet" type="button" key={`category-${facet.key}`} onClick={() => onFailureCategory(facet.key)}>
          {`category ${facet.label}: ${facet.count}`}
        </button>
      ))}
    </div>
  );
}

function SlowestRuns({ runs, workflowId = "" }) {
  if (!runs.length) {
    return null;
  }
  return (
    <div className="case-run-outliers" aria-label="Slowest case runs">
      {runs.slice(0, 3).map((run) => (
        <a className="case-run-outlier" href={evidenceHref(run, workflowId)} key={run.runId || `${run.caseId}-${run.updatedAt}`}>
          <span>{run.caseId || run.runId || "-"}</span>
          <strong>{formatDuration(run.durationMs || run.elapsedMs)}</strong>
          <code>{run.status || "-"}</code>
        </a>
      ))}
    </div>
  );
}

function ReportMetrics({ analysis, latest }) {
  const metrics = [
    ["Visible", `${analysis.summary.visible}/${analysis.summary.total}`, "filtered rows"],
    ["Failed", String(analysis.summary.failed), `${analysis.failureGroups.length} groups`],
    ["Passed", String(analysis.summary.passed), "latest local store"],
    ["Latest", latest?.status || "-", latest?.caseId || latest?.runId || "no runs"],
  ];
  return (
    <section className="case-run-report-metrics" aria-label="Run report metrics">
      {metrics.map(([label, value, detail]) => (
        <article className="case-run-report-metric" key={label}>
          <span>{label}</span>
          <strong>{value}</strong>
          <p>{detail}</p>
        </article>
      ))}
    </section>
  );
}

function CaseFocusSummary({ focus }) {
  if (!focus) {
    return null;
  }
  const latestDetail = focus.latestRunId ? `${focus.latestStatus} · ${focus.latestRunId}` : focus.latestStatus;
  return (
    <section className="case-run-focus-summary" aria-label="Case execution summary">
      <div>
        <span>Case execution summary</span>
        <strong>{focus.caseId}</strong>
        <p>{`${focus.total} runs · ${focus.passed} passed · ${focus.failed} failed`}</p>
      </div>
      <div className="case-run-focus-stats">
        <Metric label="latest" value={latestDetail || "not-run"} />
        <Metric label="longest" value={formatDuration(focus.longestDurationMs)} />
        <Metric label="updated" value={shortTime(focus.latestUpdatedAt)} />
      </div>
      {focus.latestEvidenceHref ? (
        <a className="button-link case-run-focus-evidence" href={focus.latestEvidenceHref}>Evidence</a>
      ) : null}
    </section>
  );
}

function WorkflowRunContext({ context }) {
  if (!context) {
    return null;
  }
  return (
    <section className="case-run-workflow-context" aria-label="Workflow run context">
      <div>
        <span>Workflow context</span>
        <strong>{context.workflowId}</strong>
        <p>{context.caseId ? `Focused case ${context.caseId}` : "Case run report opened from workflow context"}</p>
      </div>
      <a className="button-link" href={context.caseSetHref}>Workflow case set</a>
    </section>
  );
}

function FailureNavigator({ groups, onFailureCategory }) {
  return (
    <section className="case-run-failure-navigator" aria-label="Failure groups">
      <div className="case-run-panel-head">
        <h3>Failure triage</h3>
        <span>{`${groups.length} buckets`}</span>
      </div>
      <div className="case-run-failure-group-list">
        {groups.length ? groups.map((group) => (
          <article className="case-run-failure-triage-item" key={group.key}>
            <button type="button" onClick={() => onFailureCategory(group.key)}>
              <strong>{group.label}</strong>
              <span>{`${group.count} failed · ${group.matchedBy || "default"} · longest ${formatDuration(group.longestDurationMs)}`}</span>
              <code>{group.sampleReason || group.longestRunId || "-"}</code>
            </button>
            {group.sampleEvidenceHref ? <a className="button-link" href={group.sampleEvidenceHref}>Evidence</a> : null}
          </article>
        )) : <p className="run-history-empty">No failed groups in current run set.</p>}
      </div>
    </section>
  );
}

function FlakyCandidates({ candidates }) {
  return (
    <section className="case-run-flaky-panel" aria-label="Flaky candidates">
      <div className="case-run-panel-head">
        <h3>Flaky candidates</h3>
        <span>{`${candidates.length} mixed-history cases`}</span>
      </div>
      <div className="case-run-flaky-list">
        {candidates.length ? candidates.slice(0, 6).map((candidate) => (
          <article className="case-run-flaky-item" key={candidate.caseId}>
            <div>
              <strong>{candidate.caseId}</strong>
              <span>{candidate.operation || "case history"}</span>
            </div>
            <div className="case-run-flaky-stats">
              <code>{`score ${candidate.flakeScore}`}</code>
              <code>{`${candidate.passed} pass`}</code>
              <code>{`${candidate.failed} fail`}</code>
              <code>{`latest ${candidate.latestStatus || "-"}`}</code>
            </div>
            <div className="case-run-flaky-reasons">
              {candidate.failureReasons.length
                ? candidate.failureReasons.map((reason) => <span key={reason}>{reason}</span>)
                : <span>mixed pass and fail history</span>}
            </div>
            <div className="case-run-flaky-actions">
              <a className="button-link" href={candidate.caseRunsHref}>Runs</a>
              {candidate.latestEvidenceHref ? <a className="button-link" href={candidate.latestEvidenceHref}>Evidence</a> : null}
            </div>
          </article>
        )) : <p className="run-history-empty">No mixed pass/fail case history in the current local store.</p>}
      </div>
    </section>
  );
}

function ReportGrid({ analysis, sortOrder, onSort, onFailureCategory }) {
  const sortLabel = {
    updated_desc: "Updated ↓",
    updated_asc: "Updated ↑",
    duration_desc: "Duration ↓",
    duration_asc: "Duration ↑",
    case_asc: "Case A-Z",
    status_asc: "Status A-Z",
  }[sortOrder] || sortOrder;
  return (
    <section className="case-run-report-grid-panel">
      <div className="case-run-panel-head">
        <div>
          <h3>Report Grid</h3>
          <span>{`${analysis.grid.rows.length} rows · ${sortLabel}`}</span>
        </div>
        <div className="case-run-grid-sort">
          <ArrowDownUp size={14} aria-hidden="true" />
          <select title="Report grid sort" value={sortOrder} onChange={(event) => onSort(event.target.value)}>
            <option value="updated_desc">Updated newest</option>
            <option value="updated_asc">Updated oldest</option>
            <option value="duration_desc">Duration longest</option>
            <option value="duration_asc">Duration shortest</option>
            <option value="case_asc">Case A-Z</option>
            <option value="status_asc">Status A-Z</option>
          </select>
        </div>
      </div>
      <div className="case-run-report-table-wrap">
        <table className="case-run-report-table">
          <thead>
            <tr>
              {analysis.grid.columns.map((column) => (
                <th className={column.align === "right" ? "numeric" : ""} key={column.id}>{column.label}</th>
              ))}
            </tr>
          </thead>
          <tbody onClick={(event) => {
            const category = event.target?.dataset?.category;
            if (category) onFailureCategory(category);
          }}>
            {analysis.grid.rows.length ? analysis.grid.rows.slice(0, 80).map((row) => (
              <CaseRunGridRow row={row} key={row.id} />
            )) : (
              <tr>
                <td colSpan={analysis.grid.columns.length} className="case-run-report-empty">No matching case runs.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function CaseRunsApp() {
  const params = new URLSearchParams(window.location.search);
  const initialCaseFocus = params.get("case") || "";
  const initialWorkflowFocus = params.get("workflow") || "";
  const [payload, setPayload] = useState(null);
  const [timing, setTiming] = useState(null);
  const [incompleteBatches, setIncompleteBatches] = useState(null);
  const [caseFocus, setCaseFocus] = useState(initialCaseFocus);
  const [workflowFocus] = useState(initialWorkflowFocus);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [failureCategoryFilter, setFailureCategoryFilter] = useState("");
  const [sortOrder, setSortOrder] = useState("updated_desc");
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
  const analysis = useMemo(
    () => buildRunAnalysis(caseRuns, { query, caseId: caseFocus, workflowId: workflowFocus, status: statusFilter, failureCategory: failureCategoryFilter, failureCategoryRules: payload?.failureCategories || [], sort: sortOrder }),
    [caseRuns, query, caseFocus, workflowFocus, statusFilter, failureCategoryFilter, payload?.failureCategories, sortOrder],
  );
  const visibleRuns = analysis.visibleRuns;

  const latest = caseRuns[0];
  const warnings = payload?.warnings || [];
  const summary = latest
    ? `${analysis.summary.visible}/${analysis.summary.total} case runs · failed ${analysis.summary.failed} · latest ${latest.status || "unknown"} · ${latest.caseId || latest.runId}`
    : "0 case runs";

  return (
    <main className="app case-runs-page">
      <section className="topbar">
        <div>
          <h1>Run Analysis Center</h1>
          <p>{summary}</p>
        </div>
        <div className="actions">
          <span className="console-status-pill" role="status" title={warnings.join("\n")}>
            {message}
          </span>
          <a className="button-link" href="/">
            控制台
          </a>
          <button type="button" title="刷新" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>

      <ReportMetrics analysis={analysis} latest={latest} />
      <WorkflowRunContext context={analysis.workflowContext} />
      <CaseFocusSummary focus={analysis.caseFocus} />

      <section className="agent-test-panel">
        <div className="agent-test-section-head">
          <div>
            <h2>Case run report workbench</h2>
            <p>Dense grid, failure navigator, timing outliers, and Evidence links</p>
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
              <option value="failed">Fail</option>
              <option value="passed">Pass</option>
            </select>
          </div>
        </div>
        <Facets
          analysis={analysis}
          onStatus={setStatusFilter}
          onFailureCategory={setFailureCategoryFilter}
          onReset={() => {
            setQuery("");
            setStatusFilter("");
            setFailureCategoryFilter("");
          }}
        />
        {caseFocus ? (
          <div className="case-run-active-filter">
            <span>{`case: ${caseFocus}`}</span>
            <button type="button" onClick={() => setCaseFocus("")}>Clear case focus</button>
          </div>
        ) : null}
        {failureCategoryFilter ? (
          <div className="case-run-active-filter">
            <span>{`failure category: ${failureCategoryFilter}`}</span>
            <button type="button" onClick={() => setFailureCategoryFilter("")}>Clear</button>
          </div>
        ) : null}
        <div className="case-run-workbench-layout">
          <aside className="case-run-workbench-side">
            <FailureNavigator groups={analysis.failureTriage} onFailureCategory={setFailureCategoryFilter} />
            <FlakyCandidates candidates={analysis.flakyCandidates} />
            <section className="case-run-workbench-panel">
              <div className="case-run-panel-head">
                <h3>Timing outliers</h3>
                <span>{`${analysis.slowest.length} tracked`}</span>
              </div>
              <SlowestRuns runs={analysis.slowest} workflowId={workflowFocus} />
              <TimingSummary timing={timing} kind={timingKind} freshness={freshness} />
              <IncompleteBatches report={incompleteBatches} />
            </section>
          </aside>
          <ReportGrid analysis={analysis} sortOrder={sortOrder} onSort={setSortOrder} onFailureCategory={setFailureCategoryFilter} />
        </div>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-case-runs-root")).render(<CaseRunsApp />);
