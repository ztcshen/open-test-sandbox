import { isSkyWalkingTopology } from "./workflowStepModel.mjs";

export function buildEvidenceTimeline(payload = {}, filters = {}) {
  const items = evidenceTimelineItems(payload);
  const activeFilters = normalizeFilters(filters);
  const visibleItems = items.filter((item) => itemMatchesFilters(item, activeFilters));
  const selectedItem = visibleItems.find((item) => item.id === activeFilters.selectedId) || visibleItems[0] || null;
  return {
    items,
    visibleItems,
    selectedItem,
    activeFilters,
    facets: buildTypeFacets(items),
    summary: {
      total: items.length,
      visible: visibleItems.length,
      failed: items.filter((item) => item.tone === "failed").length,
    },
  };
}

export function buildEvidenceArtifacts(payload = {}) {
  const diagnostics = payload.caseDiagnostics || {};
  const summary = diagnostics.summary || {};
  const artifacts = [];
  for (const item of [
    ...(Array.isArray(payload.artifacts) ? payload.artifacts : []),
    ...(Array.isArray(diagnostics.artifacts) ? diagnostics.artifacts : []),
  ]) {
    addArtifact(artifacts, {
      label: item.label || item.name || item.title || "artifact",
      kind: item.kind || item.type || artifactKind(item.path || item.href || item.url || item.source || ""),
      path: item.path || item.href || item.url || item.source || "",
    });
  }
  addArtifact(artifacts, {
    label: "case bundle",
    kind: "json",
    path: summary.report_path || summary.reportPath || summary.viewer_url || summary.viewerUrl || "",
  });
  addArtifact(artifacts, {
    label: "evidence root",
    kind: "directory",
    path: summary.evidence_path || summary.evidencePath || diagnostics.evidencePath || "",
  });
  return artifacts;
}

export function buildEvidenceNavigation(context = {}) {
  const workflowId = String(context.workflowId || "").trim();
  const caseId = String(context.caseId || "").trim();
  const caseRunParams = new URLSearchParams();
  if (caseId) caseRunParams.set("case", caseId);
  if (workflowId) caseRunParams.set("workflow", workflowId);
  const caseSetParams = new URLSearchParams();
  if (workflowId) caseSetParams.set("workflow", workflowId);
  if (caseId) caseSetParams.set("case", caseId);
  return {
    caseRunsHref: caseRunParams.toString() ? `/case-runs.html?${caseRunParams.toString()}` : "/case-runs.html",
    workflowCaseSetHref: workflowId ? `/api-cases.html?${caseSetParams.toString()}` : "",
  };
}

export function buildEvidenceReproduction(payload = {}) {
  const diagnostics = payload.caseDiagnostics || {};
  const summary = diagnostics.summary || {};
  const request = diagnostics.request || {};
  const response = diagnostics.response || {};
  const assertions = diagnostics.assertions || {};
  const method = String(request.method || request.sdk_operation || request.sdkOperation || "").trim().toUpperCase();
  const path = String(request.path || request.url || request.endpoint || "").trim();
  if (!method || !path) {
    return { available: false, reason: "request evidence is missing method or path" };
  }
  const url = absoluteRequestURL(path, summary.base_url || summary.baseUrl || request.base_url || request.baseUrl || "");
  const headers = request.headers && typeof request.headers === "object" && !Array.isArray(request.headers) ? request.headers : {};
  const body = request.body ?? request.json ?? request.payload ?? "";
  const command = [
    "curl",
    "-i",
    "-X",
    method,
    ...Object.entries(headers)
      .filter(([key]) => String(key || "").trim())
      .map(([key, value]) => ["-H", shellQuote(`${key}: ${redactedHeaderValue(key, value)}`)])
      .flat(),
    body === "" || body === undefined || body === null ? "" : "--data",
    body === "" || body === undefined || body === null ? "" : shellQuote(typeof body === "string" ? body : JSON.stringify(body)),
    shellQuote(url),
  ].filter(Boolean).join(" ");
  const httpCode = response.http_code || response.httpCode || response.status || "";
  return {
    available: true,
    method,
    url,
    status: httpCode ? `HTTP ${httpCode}` : "HTTP -",
    failure: assertions.failure_reason || assertions.failureReason || summary.failure_reason || summary.failureReason || "",
    command,
  };
}

export function evidenceTimelineSearchText(item = {}) {
  return [
    item.id,
    item.type,
    item.title,
    item.detail,
    item.status,
    item.meta,
    item.preview,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function absoluteRequestURL(path, baseURL = "") {
  if (/^https?:\/\//i.test(path)) {
    return path;
  }
  const base = String(baseURL || "").trim().replace(/\/+$/, "");
  const suffix = path.startsWith("/") ? path : `/${path}`;
  return base ? `${base}${suffix}` : suffix;
}

function redactedHeaderValue(key, value) {
  const name = String(key || "").toLowerCase();
  if (["authorization", "cookie", "set-cookie", "x-api-key", "api-key", "proxy-authorization"].includes(name)) {
    return "<redacted>";
  }
  return String(value ?? "");
}

function shellQuote(value) {
  return `'${String(value ?? "").replace(/'/g, "'\\''")}'`;
}

function addArtifact(items, artifact = {}) {
  const path = String(artifact.path || "").trim();
  if (!path || items.some((item) => item.path === path)) return;
  items.push({
    id: `artifact:${items.length + 1}`,
    label: String(artifact.label || "artifact").trim(),
    kind: String(artifact.kind || artifactKind(path)).trim(),
    path,
    href: artifactHref(path),
  });
}

function artifactHref(path) {
  if (/^https?:\/\//i.test(path) || path.startsWith("/")) {
    return path;
  }
  return "";
}

function artifactKind(path) {
  const text = String(path || "").toLowerCase();
  if (text.endsWith(".json") || text.includes("application/json")) return "json";
  if (text.endsWith(".log") || text.endsWith(".txt")) return "log";
  if (text.endsWith(".har")) return "network";
  if (text.endsWith(".png") || text.endsWith(".jpg") || text.endsWith(".jpeg") || text.endsWith(".webp")) return "image";
  if (text.endsWith(".zip") || text.endsWith(".tar.gz")) return "archive";
  return "artifact";
}

function evidenceTimelineItems(payload = {}) {
  const step = payload.step || {};
  const diagnostics = payload.caseDiagnostics || {};
  const request = diagnostics.request || {};
  const response = diagnostics.response || {};
  const assertions = diagnostics.assertions || {};
  const fixture = diagnostics.fixture || {};
  const topology = step.topology || diagnostics.topology || {};
  const systems = Array.isArray(step.systems) ? step.systems : [];
  const items = [];

  if (hasObjectData(request)) {
    items.push({
      id: "request",
      type: "request",
      title: "Request",
      status: request.method || request.sdk_operation || request.sdkOperation || "request",
      detail: requestDetail(request, diagnostics.summary),
      meta: request.request_id || request.requestId || diagnostics.summary?.request_id || "",
      tone: "neutral",
      payload: request,
      preview: stringifyPayload(request),
    });
  }

  if (hasObjectData(response)) {
    const httpCode = Number(response.http_code || response.httpCode || response.status || 0);
    items.push({
      id: "response",
      type: "response",
      title: "Response",
      status: httpCode ? `HTTP ${httpCode}` : "response",
      detail: responseDetail(response),
      meta: response.request_id || response.requestId || "",
      tone: httpCode >= 400 ? "failed" : "passed",
      payload: response,
      preview: stringifyPayload(response),
    });
  }

  if (hasObjectData(assertions)) {
    const failed = failedAssertionKeys(assertions);
    const passed = assertions.passed === true || String(assertions.status || "").toLowerCase() === "passed";
    items.push({
      id: "assertions",
      type: "assertions",
      title: "Assertions",
      status: failed.length ? `${failed.length} failed` : passed ? "passed" : assertions.status || "assertions",
      detail: assertions.failure_reason || assertions.failureReason || failed.join(", ") || "tracked assertions",
      meta: assertions.failure_kind || assertions.failureKind || "",
      tone: failed.length || assertions.passed === false || String(assertions.status || "").toLowerCase() === "failed" ? "failed" : "passed",
      payload: assertions,
      preview: stringifyPayload(assertions),
    });
  }

  if (fixtureTimelineVisible(fixture)) {
    const fixtureSummary = fixture.summary || {};
    items.push({
      id: "fixture",
      type: "fixture",
      title: "Fixture",
      status: fixture.status || "configured",
      detail: `${Number(fixtureSummary.applyCount || fixture.applyRuns?.length || 0)} apply · ${Number(fixtureSummary.dependencyCount || fixture.dependencies?.length || 0)} dependencies`,
      meta: `${Number(fixtureSummary.failedCount || 0)} failed`,
      tone: Number(fixtureSummary.failedCount || 0) > 0 ? "failed" : "passed",
      payload: fixture,
      preview: stringifyPayload(fixture),
    });
  }

  if (isSkyWalkingTopology(topology)) {
    items.push({
      id: "topology",
      type: "topology",
      title: "Topology",
      status: topology.status || "topology",
      detail: topology.requestId || topology.request_id || topology.traceId || topology.trace_id || "runtime topology",
      meta: topology.traceId || topology.trace_id || "",
      tone: String(topology.status || "").toLowerCase().includes("fail") ? "failed" : "neutral",
      payload: topology,
      preview: stringifyPayload(topology),
    });
  }

  for (const system of systems.filter((item) => item?.found || item?.coreLogs?.length || item?.error)) {
    const logs = Array.isArray(system.coreLogs) ? system.coreLogs : [];
    items.push({
      id: `logs:${system.id || system.name || items.length}`,
      type: "logs",
      title: system.name || system.id || "System logs",
      status: `${logs.length} lines`,
      detail: summarizeLogLines(logs, system.error),
      meta: system.id || "",
      tone: system.error ? "failed" : "neutral",
      payload: { system: system.id, logs, error: system.error || "" },
      preview: logs.length ? logs.join("\n") : system.error || "",
    });
  }

  return items;
}

function normalizeFilters(filters = {}) {
  return {
    type: String(filters.type || "").trim(),
    query: String(filters.query || "").trim(),
    selectedId: String(filters.selectedId || "").trim(),
  };
}

function itemMatchesFilters(item, filters) {
  if (filters.type && item.type !== filters.type) {
    return false;
  }
  if (filters.query && !evidenceTimelineSearchText(item).includes(filters.query.toLowerCase())) {
    return false;
  }
  return true;
}

function buildTypeFacets(items) {
  const order = ["request", "response", "assertions", "fixture", "topology", "logs"];
  const counts = new Map();
  for (const item of items) {
    counts.set(item.type, (counts.get(item.type) || 0) + 1);
  }
  return [...counts.entries()]
    .map(([key, count]) => ({ key, label: key, count }))
    .sort((left, right) => order.indexOf(left.key) - order.indexOf(right.key) || left.key.localeCompare(right.key));
}

function hasObjectData(value) {
  return value && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length > 0;
}

function requestDetail(request, summary = {}) {
  const method = request.method || request.sdk_operation || request.sdkOperation || summary.operation || "";
  const path = request.path || request.url || request.endpoint || "";
  return [method, path].filter(Boolean).join(" ") || "request payload";
}

function responseDetail(response) {
  const duration = response.duration_ms || response.durationMs || response.elapsed_ms || response.elapsedMs;
  const size = response.size || response.size_bytes || response.sizeBytes;
  return [duration ? `${duration} ms` : "", size ? `${size} bytes` : ""].filter(Boolean).join(" · ") || "response payload";
}

function failedAssertionKeys(assertions = {}) {
  return Object.entries(assertions)
    .filter(([key, value]) => (key.endsWith("_ok") || key === "passed") && value === false)
    .map(([key]) => key);
}

function fixtureTimelineVisible(fixture = {}) {
  return Boolean(
    fixture.status ||
    (Array.isArray(fixture.applyRuns) && fixture.applyRuns.length) ||
    (Array.isArray(fixture.dependencies) && fixture.dependencies.length) ||
    (Array.isArray(fixture.upstreamSteps) && fixture.upstreamSteps.length),
  );
}

function summarizeLogLines(lines = [], error = "") {
  const first = lines.find(Boolean) || error || "system logs";
  const text = String(first).replace(/^\[?\d{4}-\d{2}-\d{2}[^\]]*\]?\s*/, "").replace(/\s+/g, " ").trim();
  return text.length > 120 ? `${text.slice(0, 120)}...` : text;
}

function stringifyPayload(value) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value || "");
  }
}
