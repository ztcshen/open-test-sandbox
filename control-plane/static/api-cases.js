const apiCaseEl = (id) => document.getElementById(id);

let apiCaseCapabilities = null;
let selectedCase = null;

function setApiCaseStatus(value) {
  apiCaseEl("apiCaseStatus").textContent = value;
}

async function apiCaseRequest(path, options = undefined) {
  const response = await fetch(path, options);
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function apiCaseKV(label, value, href = "") {
  const row = document.createElement(href ? "a" : "article");
  row.className = "api-case-kv";
  if (href) row.href = href;
  const key = document.createElement("span");
  key.textContent = label;
  const text = document.createElement("strong");
  text.textContent = value || "-";
  row.appendChild(key);
  row.appendChild(text);
  return row;
}

function formatRuntime(runtime = {}) {
  return [runtime.state || "unknown", runtime.health || runtime.message || ""].filter(Boolean).join(" · ");
}

function graphIn(graph, serviceId) {
  return (graph.edges || []).filter((edge) => edge.to === serviceId).map((edge) => edge.from).sort();
}

function graphOut(graph, serviceId) {
  return (graph.edges || []).filter((edge) => edge.from === serviceId).map((edge) => edge.to).sort();
}

function serviceName(caseDef, serviceId) {
  return (caseDef.graph?.nodes || []).find((node) => node.id === serviceId)?.displayName || serviceId;
}

function renderCaseSelector(cases) {
  const target = apiCaseEl("apiCaseList");
  target.innerHTML = "";
  if (!cases.length) {
    const empty = document.createElement("p");
    empty.textContent = "Catalog 暂未声明 API Case。";
    target.appendChild(empty);
    return;
  }
  cases.forEach((caseDef) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `api-case-select ${caseDef.id === selectedCase?.id ? "selected" : ""}`;
    button.textContent = caseDef.title || caseDef.id;
    button.addEventListener("click", () => {
      selectedCase = caseDef;
      renderApiCasePage();
    });
    target.appendChild(button);
  });
}

function renderCaseTrigger(caseDef) {
  apiCaseEl("apiCaseTitle").textContent = caseDef.title || caseDef.id || "API Case";
  apiCaseEl("apiCaseMeta").textContent = [caseDef.id, caseDef.operation].filter(Boolean).join(" · ");
  apiCaseEl("apiCaseSummary").textContent = caseDef.workflow?.displayName || caseDef.workflowId || "API Case";
  apiCaseEl("apiCaseTriggerSummary").textContent = "使用 Catalog 中声明的 case 文件、网关地址、默认参数和证据目录运行；页面不暴露请求参数。";
}

function renderCaseGraph(caseDef) {
  const graph = caseDef.graph || { nodes: [], edges: [] };
  const nodeTarget = apiCaseEl("apiCaseBoundary");
  const edgeTarget = apiCaseEl("apiCaseBoundaryEdges");
  nodeTarget.innerHTML = "";
  edgeTarget.innerHTML = "";
  apiCaseEl("apiCaseBoundaryMeta").textContent = `${graph.nodes?.length || 0} services · ${graph.edges?.length || 0} DAG edges`;

  (graph.nodes || []).forEach((service) => {
    const node = document.createElement(service.href ? "a" : "article");
    node.className = `workflow-graph-node ${service.role || "unknown"}`;
    if (service.href) node.href = service.href;
    const name = document.createElement("strong");
    name.textContent = service.displayName || service.id;
    const meta = document.createElement("span");
    meta.textContent = [service.role || "service", service.port ? `:${service.port}` : ""].filter(Boolean).join(" · ");
    node.appendChild(name);
    node.appendChild(meta);
    nodeTarget.appendChild(node);
  });

  (graph.edges || []).forEach((edgeDef) => {
    const edge = document.createElement("article");
    edge.className = "workflow-graph-edge";
    const from = document.createElement("strong");
    from.textContent = serviceName(caseDef, edgeDef.from);
    const arrow = document.createElement("span");
    arrow.textContent = "->";
    const to = document.createElement("strong");
    to.textContent = serviceName(caseDef, edgeDef.to);
    edge.appendChild(from);
    edge.appendChild(arrow);
    edge.appendChild(to);
    edgeTarget.appendChild(edge);
  });
}

function renderCaseServices(caseDef) {
  const target = apiCaseEl("apiCaseServices");
  const graph = caseDef.graph || { nodes: [], edges: [] };
  target.innerHTML = "";
  (graph.nodes || []).forEach((service) => {
    const card = document.createElement("section");
    card.className = "api-case-service-card";
    const title = document.createElement("div");
    title.className = "section-head compact-head";
    const heading = document.createElement("h3");
    heading.textContent = service.displayName || service.id;
    title.appendChild(heading);
    card.appendChild(title);
    const grid = document.createElement("div");
    grid.className = "api-case-capability-grid";
    [
      ["service", service.id, service.href],
      ["role", service.role],
      ["port", service.port ? `:${service.port}` : "-"],
      ["runtime", formatRuntime(service.runtime)],
      ["in", graphIn(graph, service.id).map((id) => serviceName(caseDef, id)).join(", ") || "-"],
      ["out", graphOut(graph, service.id).map((id) => serviceName(caseDef, id)).join(", ") || "-"],
    ].forEach(([label, value, href]) => grid.appendChild(apiCaseKV(label, value, href)));
    card.appendChild(grid);
    target.appendChild(card);
  });
}

function renderApiCaseResult(payload) {
  const target = apiCaseEl("apiCaseResult");
  target.innerHTML = "";
  target.className = `api-case-result ${payload.ok ? "passed" : "failed"} ${payload.dryRun ? "dry-run" : "real-run"}`;
  const data = payload.report || payload.summary || {};
  const title = document.createElement("strong");
  title.textContent = payload.dryRun
    ? `DRY-RUN · ${data.case_id || data.CaseID || "case"}`
    : `${data.status || "fail"} · ${data.run_id || "-"}`;
  const meta = document.createElement("p");
  meta.textContent = payload.dryRun
    ? `trace ${data.trace_id || "-"} · ${data.operation || "-"}`
    : `http ${data.actual_http_code || "-"} · request ${data.request_id || "-"}`;
  target.appendChild(title);
  target.appendChild(meta);
  if (payload.viewerUrl) {
    const link = document.createElement("a");
    link.className = "button-link";
    link.href = payload.viewerUrl;
    link.textContent = "打开 Evidence";
    target.appendChild(link);
  }
}

function casePayload(caseDef) {
  return {
    dryRun: false,
    casePath: caseDef.casePath || "",
    baseUrl: caseDef.baseUrl || "",
    evidenceDir: caseDef.evidenceDir || ".runtime/cases",
    timeoutSeconds: caseDef.timeoutSeconds || 90,
    overrides: caseDef.defaultOverrides || {},
  };
}

async function runSelectedCase() {
  if (!selectedCase) return;
  const runButton = apiCaseEl("runApiCase");
  runButton.disabled = true;
  setApiCaseStatus("running...");
  try {
    const payload = await apiCaseRequest("/api/cases/run", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(casePayload(selectedCase)),
    });
    renderApiCaseResult(payload);
    setApiCaseStatus(payload.ok ? "ready" : "case failed");
  } catch (error) {
    renderApiCaseResult({ ok: false, dryRun: false, summary: { status: "fail", failure_reason: error.message } });
    setApiCaseStatus(error.message);
  } finally {
    runButton.disabled = false;
  }
}

function renderApiCasePage() {
  const cases = apiCaseCapabilities?.cases || [];
  selectedCase = selectedCase || cases[0] || null;
  renderCaseSelector(cases);
  if (!selectedCase) {
    apiCaseEl("runApiCase").disabled = true;
    return;
  }
  apiCaseEl("runApiCase").disabled = false;
  renderCaseTrigger(selectedCase);
  renderCaseGraph(selectedCase);
  renderCaseServices(selectedCase);
}

async function loadApiCases() {
  setApiCaseStatus("loading...");
  apiCaseCapabilities = await apiCaseRequest("/api/cases/capabilities");
  const requested = new URLSearchParams(window.location.search).get("case");
  selectedCase = (apiCaseCapabilities.cases || []).find((caseDef) => caseDef.id === requested) || null;
  renderApiCasePage();
  setApiCaseStatus("ready");
}

apiCaseEl("runApiCase").addEventListener("click", () => runSelectedCase());
loadApiCases().catch((error) => setApiCaseStatus(error.message));
