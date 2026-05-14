const agentRunEl = (id) => document.getElementById(id);

function setAgentRunStatus(value) {
  agentRunEl("agentRunStatus").textContent = value;
}

async function agentRunRequest(path) {
  const response = await fetch(path);
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function agentRunKV(label, value) {
  const card = document.createElement("article");
  const key = document.createElement("span");
  key.textContent = label;
  const text = document.createElement("strong");
  text.textContent = value || "-";
  card.appendChild(key);
  card.appendChild(text);
  return card;
}

function renderAgentRunDetail(run, runs = []) {
  agentRunEl("agentRunTitle").textContent = run.runId || "-";
  renderAgentRunSummary(run);
  renderAgentRunDiagnosis(run, runs);
  renderAgentRunEvidence(run);
}

function renderAgentRunSummary(run) {
  const target = agentRunEl("agentRunSummary");
  target.innerHTML = "";
  [
    ["status", run.status],
    ["failureKind", run.failureKind || "none"],
    ["profile", run.profileId],
    ["service", run.resolvedServiceId],
    ["ref", run.ref],
    ["commit", run.commitId],
  ].forEach(([label, value]) => target.appendChild(agentRunKV(label, value)));
}

function renderAgentRunDiagnosis(run, runs = []) {
  const diagnosis = run.diagnosis || {};
  const blockedReport = run.blockedReport || {};
  const target = agentRunEl("agentRunDiagnosis");
  target.innerHTML = "";
  const entries = [
    ["reason", diagnosis.reason || run.failureKind || "no failure"],
    ["next step", diagnosis.nextStep || "inspect the Evidence bundle before escalating"],
    ["request id", diagnosis.requestId],
    ["service", diagnosis.service || run.resolvedServiceId],
    ["log excerpt", diagnosis.logExcerpt],
    ["blocked status", blockedReport.status],
    ["blocked reason", blockedReport.reason],
  ].filter(([, value]) => value);
  entries.forEach(([label, value]) => target.appendChild(agentRunFact(label, value)));
  const relatedCount = renderAgentRunRelatedRuns(target, run, runs);
  agentRunEl("agentRunDiagnosisMeta").textContent = `${entries.length + relatedCount} diagnosis fields`;
}

function renderAgentRunRelatedRuns(target, run, runs = []) {
  const relatedRuns = runs.filter((candidate) => candidate.profileId === run.profileId && candidate.runId !== run.runId).slice(0, 5);
  if (!relatedRuns.length) return 0;
  const item = document.createElement("article");
  item.className = "agent-run-detail-item agent-run-related-runs";
  const label = document.createElement("span");
  label.textContent = "same profile recent runs";
  const list = document.createElement("div");
  list.className = "agent-run-related-list";
  relatedRuns.forEach((candidate) => {
    const link = document.createElement("a");
    link.className = "agent-run-related-link";
    link.href = `/agent-run.html?runId=${encodeURIComponent(candidate.runId || "")}`;
    link.textContent = `${candidate.runId || "-"} (${candidate.status || "unknown"}${candidate.failureKind ? ` · ${candidate.failureKind}` : ""})`;
    list.appendChild(link);
  });
  item.appendChild(label);
  item.appendChild(list);
  target.appendChild(item);
  return 1;
}

function renderAgentRunEvidence(run) {
  const target = agentRunEl("agentRunEvidence");
  target.innerHTML = "";
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
    ["started", formatAgentRunTime(run.startedAt)],
    ["ended", formatAgentRunTime(run.endedAt)],
  ].filter(([, value]) => value);
  agentRunEl("agentRunEvidenceMeta").textContent = `${entries.length} evidence pointers`;
  if (!entries.length) {
    target.appendChild(agentRunEmpty("此 Agent run 没有 Evidence bundle 路径。"));
    return;
  }
  entries.forEach(([label, value]) => target.appendChild(agentRunFact(label, value, "code")));
}

function renderAgentRunMissing(runId, runs) {
  agentRunEl("agentRunTitle").textContent = runId ? `未找到 run · ${runId}` : "缺少 runId";
  const summary = agentRunEl("agentRunSummary");
  summary.innerHTML = "";
  summary.appendChild(agentRunKV("status", "missing"));
  summary.appendChild(agentRunKV("requested", runId || "-"));
  summary.appendChild(agentRunKV("recent runs", String(runs.length)));
  agentRunEl("agentRunDiagnosisMeta").textContent = "恢复入口";
  const diagnosis = agentRunEl("agentRunDiagnosis");
  diagnosis.innerHTML = "";
  diagnosis.appendChild(agentRunFact("next step", "返回 Agent Test Kit 选择一个已持久化 run。"));
  agentRunEl("agentRunEvidenceMeta").textContent = "0 evidence pointers";
  agentRunEl("agentRunEvidence").innerHTML = "";
}

function agentRunFact(label, value, mode = "text") {
  const item = document.createElement("article");
  item.className = "agent-run-detail-item";
  const key = document.createElement("span");
  key.textContent = label;
  const body = document.createElement(mode === "code" ? "code" : "p");
  body.textContent = value || "-";
  item.appendChild(key);
  item.appendChild(body);
  return item;
}

function agentRunEmpty(text) {
  const empty = document.createElement("div");
  empty.className = "agent-empty";
  empty.textContent = text;
  return empty;
}

function formatAgentRunTime(value) {
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

async function refreshAgentRunDetail() {
  const runId = new URLSearchParams(window.location.search).get("runId") || "";
  setAgentRunStatus("refreshing...");
  const payload = await agentRunRequest("/api/agent-test");
  const runs = payload.agentRuns || [];
  const run = runs.find((candidate) => candidate.runId === runId);
  if (!run) {
    renderAgentRunMissing(runId, runs);
    setAgentRunStatus("missing");
    return;
  }
  renderAgentRunDetail(run, runs);
  setAgentRunStatus(run.status || "ready");
}

refreshAgentRunDetail().catch((error) => {
  renderAgentRunMissing(new URLSearchParams(window.location.search).get("runId") || "", []);
  setAgentRunStatus(error.message);
});
