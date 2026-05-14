const replayEvidenceEl = (id) => document.getElementById(id);

function setReplayEvidenceStatus(value) {
  replayEvidenceEl("replayEvidenceStatus").textContent = value;
}

async function replayEvidenceRequest(path) {
  const response = await fetch(path);
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function formatReplayEvidenceTime(value) {
  if (!value) return "-";
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

function safeReplayEvidenceJSON(value) {
  if (!value) return {};
  try {
    return JSON.parse(value);
  } catch {
    return {};
  }
}

function replayEvidenceKV(label, value) {
  const row = document.createElement("article");
  const key = document.createElement("span");
  key.textContent = label;
  const text = document.createElement("strong");
  text.textContent = value || "-";
  row.appendChild(key);
  row.appendChild(text);
  return row;
}

function renderReplayEvidenceRun(run, evidence) {
  const summary = safeReplayEvidenceJSON(run.summaryJson);
  replayEvidenceEl("replayEvidenceTrace").textContent = run.traceId || evidence.traceId || "-";
  replayEvidenceEl("replayEvidenceRunMeta").textContent = `${run.httpStatus || summary.httpStatus || "-"} · ${formatReplayEvidenceTime(run.createdAt)}`;
  const target = replayEvidenceEl("replayEvidenceRun");
  target.innerHTML = "";
  [
    ["trace", run.traceId || evidence.traceId],
    ["target", run.targetUrl || evidence.request?.targetUrl || summary.targetUrl],
    ["scenario", run.scenario || summary.scenario || "-"],
    ["evidence", run.evidencePath || "-"],
  ].forEach(([label, value]) => target.appendChild(replayEvidenceKV(label, value)));
}

function renderReplayEvidenceRequest(evidence) {
  const target = replayEvidenceEl("replayEvidenceRequest");
  target.innerHTML = "";
  const request = evidence.request || {};
  const response = evidence.response || {};
  replayEvidenceEl("replayEvidenceRequestMeta").textContent = `${request.method || "-"} · http ${response.httpStatus || "-"}`;
  [
    ["method", request.method],
    ["url", request.targetUrl],
    ["http status", String(response.httpStatus || "-")],
    ["body", response.bodySummary || "-"],
  ].forEach(([label, value]) => target.appendChild(replayEvidenceKV(label, value)));
}

function renderReplayEvidenceSystems(evidence) {
  const target = replayEvidenceEl("replayEvidenceSystems");
  target.innerHTML = "";
  const systems = evidence.systems || [];
  replayEvidenceEl("replayEvidenceSystemsMeta").textContent = `${systems.filter((system) => system.found).length}/${systems.length} matched`;
  if (!systems.length) {
    const empty = document.createElement("div");
    empty.className = "empty-note";
    empty.textContent = "暂无系统证据。";
    target.appendChild(empty);
    return;
  }
  systems.forEach((system) => {
    const card = document.createElement("article");
    card.className = "replay-evidence-system-card";
    const head = document.createElement("div");
    const title = document.createElement("strong");
    title.textContent = system.name || system.id || "-";
    const status = document.createElement("span");
    status.className = `status-pill ${system.found ? "passed" : ""}`;
    status.textContent = system.found ? "matched" : "empty";
    head.appendChild(title);
    head.appendChild(status);
    const lines = document.createElement("pre");
    lines.textContent = (system.coreLogs || []).slice(0, 4).join("\n") || system.note || "No matching logs";
    card.appendChild(head);
    card.appendChild(lines);
    target.appendChild(card);
  });
}

function renderReplayEvidence(payload) {
  renderReplayEvidenceRun(payload.run || {}, payload.evidence || {});
  renderReplayEvidenceRequest(payload.evidence || {});
  renderReplayEvidenceSystems(payload.evidence || {});
}

async function refreshReplayEvidence() {
  const traceId = new URLSearchParams(window.location.search).get("traceId") || "";
  if (!traceId) {
    throw new Error("traceId is required");
  }
  setReplayEvidenceStatus("refreshing...");
  const payload = await replayEvidenceRequest(`/api/replay/evidence?traceId=${encodeURIComponent(traceId)}`);
  renderReplayEvidence(payload);
  setReplayEvidenceStatus("ready");
}

refreshReplayEvidence().catch((error) => {
  replayEvidenceEl("replayEvidenceTrace").textContent = error.message;
  setReplayEvidenceStatus("failed");
});
