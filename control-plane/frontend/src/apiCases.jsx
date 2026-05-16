import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";

async function requestJSON(path, options = undefined) {
  const response = await fetch(path, {
    headers: { Accept: "application/json", ...(options?.headers || {}) },
    ...options,
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function runtimeText(runtime = {}) {
  return [runtime.state || "unknown", runtime.health || runtime.message || ""].filter(Boolean).join(" · ");
}

function graphInputs(graph, serviceId) {
  return (graph.edges || []).filter((edge) => edge.to === serviceId).map((edge) => edge.from).sort();
}

function graphOutputs(graph, serviceId) {
  return (graph.edges || []).filter((edge) => edge.from === serviceId).map((edge) => edge.to).sort();
}

function serviceName(caseDef, serviceId) {
  return (caseDef.graph?.nodes || []).find((node) => node.id === serviceId)?.displayName || serviceId;
}

function selectedCaseFromPayload(payload, preferredID = "") {
  const cases = payload?.cases || [];
  const requested = new URLSearchParams(window.location.search).get("case");
  return cases.find((caseDef) => caseDef.id === preferredID) || cases.find((caseDef) => caseDef.id === requested) || cases[0] || null;
}

function caseRunPayload(caseDef) {
  return {
    casePath: caseDef.casePath || "",
    baseUrl: caseDef.baseUrl || "",
    evidenceDir: caseDef.evidenceDir || ".runtime/cases",
    timeoutSeconds: caseDef.timeoutSeconds || 90,
    overrides: caseDef.defaultOverrides || {},
  };
}

function KeyValue({ label, value, href }) {
  const body = (
    <>
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </>
  );
  if (href) {
    return (
      <a className="api-case-kv" href={href}>
        {body}
      </a>
    );
  }
  return <article className="api-case-kv">{body}</article>;
}

function CaseSelector({ cases, selectedCase, onSelect }) {
  if (!cases.length) {
    return <p>Catalog 暂未声明 API Case。</p>;
  }
  return (
    <div className="api-case-list">
      {cases.map((caseDef) => (
        <button
          type="button"
          className={`api-case-select ${caseDef.id === selectedCase?.id ? "selected" : ""}`}
          key={caseDef.id}
          onClick={() => onSelect(caseDef)}
        >
          {caseDef.title || caseDef.id}
        </button>
      ))}
    </div>
  );
}

function CaseResult({ result }) {
  if (!result) {
    return <div className="api-case-result">ready</div>;
  }
  const data = result.report || result.summary || {};
  const title = `${data.status || "fail"} · ${data.run_id || "-"}`;
  const meta = `http ${data.actual_http_code || "-"} · request ${data.request_id || "-"}`;
  return (
    <div className={`api-case-result ${result.ok ? "passed" : "failed"}`}>
      <strong>{title}</strong>
      <p>{meta}</p>
      {result.viewerUrl ? (
        <a className="button-link" href={result.viewerUrl}>
          打开 Evidence
        </a>
      ) : null}
    </div>
  );
}

function LatestRunSummary({ caseDef }) {
  const latestRun = caseDef?.latestRun || null;
  return (
    <div className="api-case-capability-grid">
      <KeyValue label="runs" value={String(caseDef?.runCount || 0)} />
      <KeyValue
        label="latest"
        value={latestRun ? [latestRun.status || "unknown", latestRun.failureReason].filter(Boolean).join(" · ") : "no run"}
        href={latestRun?.runId ? `/evidence-viewer.html?${new URLSearchParams({ caseRun: latestRun.runId, caseId: caseDef.id }).toString()}` : ""}
      />
      <KeyValue label="case run" value={latestRun?.caseRunId || "-"} />
      <KeyValue label="elapsed" value={latestRun?.elapsedMs ? `${latestRun.elapsedMs}ms` : "-"} />
    </div>
  );
}

function CaseServices({ caseDef }) {
  const graph = caseDef?.graph || { nodes: [], edges: [] };
  return (
    <div className="api-case-service-list">
      {(graph.nodes || []).map((service) => (
        <section className="api-case-service-card" key={service.id}>
          <div className="section-head compact-head">
            <h3>{service.displayName || service.id}</h3>
          </div>
          <div className="api-case-capability-grid">
            <KeyValue label="service" value={service.id} href={service.href} />
            <KeyValue label="role" value={service.role} />
            <KeyValue label="port" value={service.port ? `:${service.port}` : "-"} />
            <KeyValue label="runtime" value={runtimeText(service.runtime)} />
            <KeyValue label="in" value={graphInputs(graph, service.id).map((id) => serviceName(caseDef, id)).join(", ") || "-"} />
            <KeyValue label="out" value={graphOutputs(graph, service.id).map((id) => serviceName(caseDef, id)).join(", ") || "-"} />
          </div>
        </section>
      ))}
    </div>
  );
}

function CaseBoundary({ caseDef }) {
  const graph = caseDef?.graph || { nodes: [], edges: [] };
  return (
    <>
      <div className="workflow-graph-nodes">
        {(graph.nodes || []).map((service) => {
          const className = `workflow-graph-node ${service.role || "unknown"}`;
          const body = (
            <>
              <strong>{service.displayName || service.id}</strong>
              <span>{[service.role || "service", service.port ? `:${service.port}` : ""].filter(Boolean).join(" · ")}</span>
            </>
          );
          return service.href ? (
            <a className={className} href={service.href} key={service.id}>
              {body}
            </a>
          ) : (
            <article className={className} key={service.id}>
              {body}
            </article>
          );
        })}
      </div>
      <div className="workflow-graph-edges">
        {(graph.edges || []).map((edge) => (
          <article className="workflow-graph-edge" key={`${edge.from}-${edge.to}`}>
            <strong>{serviceName(caseDef, edge.from)}</strong>
            <span>{"->"}</span>
            <strong>{serviceName(caseDef, edge.to)}</strong>
          </article>
        ))}
      </div>
    </>
  );
}

function ApiCasesApp() {
  const [capabilities, setCapabilities] = useState(null);
  const [selectedCase, setSelectedCase] = useState(null);
  const [result, setResult] = useState(null);
  const [status, setStatus] = useState("loading");

  async function loadCapabilities(preferredCaseID = "", nextStatus = "ready") {
    setStatus("loading...");
    try {
      const payload = await requestJSON("/api/cases/capabilities");
      setCapabilities(payload);
      setSelectedCase(selectedCaseFromPayload(payload, preferredCaseID));
      setStatus(nextStatus);
    } catch (error) {
      setStatus(error.message);
    }
  }

  async function runSelectedCase() {
    if (!selectedCase) return;
    setStatus("running...");
    try {
      const payload = await requestJSON("/api/cases/run", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(caseRunPayload(selectedCase)),
      });
      setResult(payload);
      await loadCapabilities(selectedCase.id, payload.ok ? "ready" : "case failed");
    } catch (error) {
      setResult({ ok: false, summary: { status: "fail", failure_reason: error.message } });
      setStatus(error.message);
    }
  }

  useEffect(() => {
    loadCapabilities();
  }, []);

  const cases = capabilities?.cases || [];
  const graph = selectedCase?.graph || { nodes: [], edges: [] };
  const caseMeta = useMemo(() => [selectedCase?.id, selectedCase?.operation].filter(Boolean).join(" · "), [selectedCase]);
  const pageSummary = selectedCase?.workflow?.displayName || selectedCase?.workflowId || "API Case";

  return (
    <main className="app api-case-page">
      <section className="topbar">
        <div>
          <h1>API Case 工作台</h1>
          <p>{pageSummary}</p>
        </div>
        <div className="actions">
          <span className="workflow-step-status-pill" role="status">
            {status}
          </span>
          <a className="button-link" href="/">
            控制台
          </a>
          <a className="button-link" href="/dashboard.html">
            环境大盘
          </a>
          <a className="button-link" href="/service-inventory.html">
            服务清单
          </a>
        </div>
      </section>

      <section className="api-case-shell">
        <section className="api-case-panel api-case-control-panel">
          <div className="section-head">
            <div>
              <h2>{selectedCase?.title || selectedCase?.id || "API Case"}</h2>
              <p>{caseMeta || "loading"}</p>
            </div>
          </div>
          <CaseSelector cases={cases} selectedCase={selectedCase} onSelect={(caseDef) => {
            setSelectedCase(caseDef);
            setResult(null);
          }} />
          <LatestRunSummary caseDef={selectedCase} />
          <div className="api-case-trigger">
            <p>使用 Catalog 中声明的 case 文件、网关地址、默认参数和证据目录运行；页面不暴露请求参数。</p>
            <button className="primary-action" type="button" disabled={!selectedCase || status === "running..."} onClick={runSelectedCase}>
              运行 Case
            </button>
          </div>
          <CaseResult result={result} />
        </section>

        <section className="api-case-panel">
          <div className="section-head">
            <div>
              <h2>相关服务</h2>
              <p>由 Catalog 中当前 Case 的 DAG 节点生成。</p>
            </div>
          </div>
          <CaseServices caseDef={selectedCase} />
        </section>

        <section className="api-case-panel api-case-graph-panel">
          <div className="section-head">
            <div>
              <h2>Case 服务边界</h2>
              <p>{`${graph.nodes?.length || 0} services · ${graph.edges?.length || 0} DAG edges`}</p>
            </div>
          </div>
          <CaseBoundary caseDef={selectedCase} />
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-api-cases-root")).render(<ApiCasesApp />);
