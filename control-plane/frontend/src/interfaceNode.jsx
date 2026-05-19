import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { TopologyDiagram, topologyEdges, topologyNodes } from "./topologyView.jsx";
import { isSkyWalkingTopology, unavailableSkyWalkingTopology } from "./workflowStepModel.mjs";

function queryParam(name) {
  return new URLSearchParams(window.location.search).get(name) || "";
}

function queryContextParams() {
  const current = new URLSearchParams(window.location.search);
  const params = new URLSearchParams();
  ["id", "runId", "flowId", "workflowRunId", "workflowId", "workflow", "case", "caseId", "stepId", "step"].forEach((key) => {
    const value = current.get(key);
    if (value) params.set(key, value);
  });
  return params;
}

function workflowCaseSetHref() {
  const params = queryContextParams();
  const workflowID = params.get("workflow") || params.get("workflowId") || "";
  if (!workflowID) return "";
  const caseID = params.get("case") || "";
  const out = new URLSearchParams({ workflow: workflowID });
  if (caseID) out.set("case", caseID);
  return `/api-cases.html?${out.toString()}`;
}

function pageMode() {
  const path = window.location.pathname;
  if (path.includes("history")) return "history";
  if (path.includes("fields")) return "fields";
  return "main";
}

function text(value, defaultValue = "-") {
  const out = String(value ?? "").trim();
  return out || defaultValue;
}

function copyText(payload, key, defaultValue) {
  return payload?.presentation?.copy?.[key] || defaultValue;
}

function tail(value, length = 12) {
  const out = text(value);
  return out.length <= length ? out : `...${out.slice(-length)}`;
}

function prettyJSON(value) {
  if (value === undefined || value === null || value === "") return "-";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

function runMatchesContext(run, context) {
  if (!run || !context) return false;
  const runID = context.runId || "";
  const stepID = context.stepId || "";
  if (runID && run.runId !== runID) return false;
  if (context.flowId && run.workflowId !== context.flowId) return false;
  if (!stepID) return true;
  return String(run.requestSummary?.stepId || "").trim() === stepID;
}

function contextualCaseID(cases, context) {
  if (!context) return "";
  const exact = cases.find((item) => runMatchesContext(item.latestRun, context));
  if (exact?.id) return exact.id;
  const runID = context.runId || "";
  if (!runID) return "";
  return cases.find((item) => item.latestRun?.runId === runID)?.id || "";
}

function duration(ms) {
  const value = Number(ms || 0);
  if (!Number.isFinite(value) || value <= 0) return "-";
  if (value < 1000) return `${Math.round(value)}ms`;
  return `${(value / 1000).toFixed(1)}s`;
}

function runElapsedMs(run) {
  if (!run) return 0;
  const direct = Number(run.elapsedMs || 0);
  if (Number.isFinite(direct) && direct > 0) return direct;
  try {
    const summary = JSON.parse(run.summaryJson || "{}");
    const parsed = Number(summary.elapsedMs || summary.elapsed_ms || 0);
    return Number.isFinite(parsed) ? parsed : 0;
  } catch {
    return 0;
  }
}

function caseGroupKey(item) {
  const type = String(item?.caseType || "").trim().toLowerCase();
  return ["success", "pass", "positive"].includes(type) ? "success" : "failure";
}

function caseNumber(cases, item) {
  if (!item) return "";
  const groupKey = caseGroupKey(item);
  const group = cases.filter((candidate) => caseGroupKey(candidate) === groupKey);
  const index = group.findIndex((candidate) => candidate.id && item.id ? candidate.id === item.id : candidate === item);
  return `${groupKey === "success" ? "S" : "F"}${String(Math.max(index, 0) + 1).padStart(2, "0")}`;
}

function outcomeLabel(item) {
  const run = item?.latestRun;
  if (!run) return "no run";
  const status = String(run.status || "").trim().toLowerCase();
  const passed = ["pass", "passed", "success", "succeeded"].includes(status);
  if (caseGroupKey(item) === "failure") {
    return passed ? "命中预期失败" : "未命中预期失败";
  }
  return run.status || "unknown";
}

function RunBadge({ item }) {
  const run = item?.latestRun;
  const tone = run?.status === "pass" || run?.status === "passed" ? "good" : run ? "bad" : "warn";
  return <span className={`react-pill ${tone}`}>{outcomeLabel(item)}</span>;
}

function CaseTopology({ run, payload }) {
  const rawTopology = run?.topology || run?.traceTopology || {};
  const topology = isSkyWalkingTopology(rawTopology) ? rawTopology : unavailableSkyWalkingTopology();
  const edges = topologyEdges(topology);
  const nodes = topologyNodes(topology, edges);
  if (!topology.status && !topology.traceId && !nodes.length) return null;
  return (
    <section className="workflow-step-topology-graph interface-node-case-topology">
      <div className="section-head workflow-step-topology-head">
        <div>
          <h4>{copyText(payload, "topologyTitle", "SkyWalking 自动拓扑")}</h4>
          <p>{`${nodes.length} nodes · ${edges.length} edges · ${topology.status || "unavailable"}`}</p>
        </div>
        {topology.traceId ? <code>{topology.traceId}</code> : null}
      </div>
      <TopologyDiagram topology={topology} markerPrefix={`interface-node-${run?.caseRunId || run?.runId || "case"}`} emptyLabel={copyText(payload, "topologyEmpty", "SkyWalking 暂无可绘制链路。")} />
      <div className="workflow-step-topology-edges">
        {edges.length ? edges.map((edge, index) => (
          <article className={`workflow-step-topology-edge ${edge.kind || ""}`} key={`${edge.source}-${edge.target}-${index}`}>
            <strong>{edge.source || "-"}</strong>
            <span>{"->"}</span>
            <strong>{edge.target || "-"}</strong>
            <code>{edge.component || edge.sourceComponent || edge.kind || "-"}</code>
          </article>
        )) : <p className="dashboard-empty">{copyText(payload, "topologyEdgesEmpty", "当前接口运行没有 SkyWalking 边。")}</p>}
      </div>
    </section>
  );
}

function Stat({ label, value, title }) {
  return (
    <div title={text(title || value)}>
      <span>{label}</span>
      <strong>{text(value)}</strong>
    </div>
  );
}

function Panel({ title, subtitle, className = "", children }) {
  return (
    <section className={`environment-node-detail-panel interface-node-panel ${className}`.trim()}>
      <div className="dashboard-section-head">
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      {children}
    </section>
  );
}

function Summary({ rows }) {
  return (
    <div className="interface-run-summary interface-node-case-run-summary">
      {rows.map(([label, value, tone]) => (
        <article className={["interface-run-kv", tone].filter(Boolean).join(" ")} key={label}>
          <span>{label}</span>
          <strong>{text(value)}</strong>
        </article>
      ))}
    </div>
  );
}

function RequestTemplatePanel({ payload }) {
  const templates = Array.isArray(payload.requestTemplates) ? payload.requestTemplates : [];
  const requestFields = payload.fields?.request || [];
  return (
    <Panel
      title={copyText(payload, "requestTemplateTitle", "公共模板参数")}
      subtitle={templates.length ? copyText(payload, "requestTemplateSubtitle", "来自 interface_node_request_template，Case 只维护差异 Patch") : copyText(payload, "requestTemplateEmptySubtitle", "尚未登记公共请求模板，先按接口字段契约展示公共参数骨架")}
      className="interface-node-request-template-panel"
    >
      <div className={`interface-node-request-template-body ${templates.length ? "" : "no-template"}`.trim()}>
        <div className="interface-node-request-template-fields">
          <span>{copyText(payload, "requestFieldsLabel", "公共参数")}</span>
          {requestFields.length ? requestFields.map((field) => (
            <div className="interface-node-request-template-field" key={field.id || field.fieldPath}>
              <strong>{field.displayName || field.fieldPath || field.id || "-"}</strong>
              <code>{field.fieldPath || "-"}</code>
              <span>{[field.dataType || "unknown", field.required ? "required" : "optional", field.bindable ? "bindable" : ""].filter(Boolean).join(" · ")}</span>
            </div>
          )) : <p className="dashboard-empty">{copyText(payload, "requestFieldsEmpty", "当前接口节点还没有登记请求字段。")}</p>}
        </div>
        <div className="interface-node-request-template-list">
          <span>{copyText(payload, "requestTemplateJSONLabel", "模板 JSON")}</span>
          {templates.length ? templates.map((template) => (
            <article className="interface-node-request-template-card" key={template.id}>
              <div className="interface-node-request-template-card-top">
                <strong>{template.name || template.id || copyText(payload, "requestTemplateDefaultName", "公共请求模板")}</strong>
                <code>{[template.id || "", template.version || "", template.status || ""].filter(Boolean).join(" · ") || "-"}</code>
              </div>
              <pre>{prettyJSON(template.templateJson || template.template_json || "{}")}</pre>
            </article>
          )) : <p className="dashboard-empty">{copyText(payload, "requestTemplateEmpty", "未找到 interface_node_request_template 记录。新增必填字段时，应优先补公共请求模板，再让 Case Patch 表达差异。")}</p>}
        </div>
      </div>
    </Panel>
  );
}

function AttentionPanel({ attention, payload }) {
  if (!attention || !attention.blockerCount) return null;
  const blockers = Array.isArray(attention.blockers) ? attention.blockers : [];
  return (
    <Panel title={copyText(payload, "attentionTitle", "Attention")} subtitle={`${attention.blockerCount} ${copyText(payload, "attentionSubtitleSuffix", "个 required case 需要处理")}`} className="interface-node-admission">
      <div className="interface-node-admission-blockers">
        {blockers.slice(0, 3).map((blocker) => (
          <article className="interface-node-admission-blocker" key={blocker.caseId || blocker.title}>
            <div>
              <strong>{blocker.title || blocker.caseId || "required case"}</strong>
              <span className={`react-pill ${blocker.status === "failed" ? "bad" : "warn"}`}>{blocker.status || "blocked"}</span>
            </div>
            <code>{[blocker.caseId, blocker.runId, blocker.failureKind].filter(Boolean).join(" · ") || "-"}</code>
            <p>{blocker.failureReason || "required case is not admitted"}</p>
          </article>
        ))}
      </div>
    </Panel>
  );
}

function Dependencies({ item, payload }) {
  const dependencies = item?.dependencies || [];
  if (!dependencies.length) return null;
  return (
    <div className="interface-node-case-dependencies">
      <span>{copyText(payload, "dependenciesLabel", "前置数据")}</span>
      {dependencies.map((dependency) => (
        <div className="interface-node-case-dependency" key={dependency.id || dependency.fixtureProfileId}>
          <strong>{dependency.profile?.name || dependency.fixtureProfileId || dependency.id}</strong>
          <code>{[dependency.fixtureProfileId, dependency.required ? "required" : "optional", (dependency.tableBindings || []).map((binding) => `${binding.schemaName}.${binding.tableName}`).join(", ")].filter(Boolean).join(" · ")}</code>
          <pre>{prettyJSON(dependency.mappingsJson || "[]")}</pre>
        </div>
      ))}
    </div>
  );
}

function CaseDetail({ item, cases, onRunCase, payload }) {
  if (!item) {
    return (
      <article className="interface-node-case-detail">
        <p className="dashboard-empty">{copyText(payload, "emptyCases", "当前接口节点还没有配置测试用例。")}</p>
      </article>
    );
  }
  const run = item.latestRun || {};
  return (
    <article className="interface-node-case-detail">
      <div className="interface-node-case-detail-top">
        <div>
          <h3>{`${caseNumber(cases, item)} · ${item.title || item.id}`}</h3>
          <code>{item.id || "-"}</code>
        </div>
        <RunBadge item={item} />
      </div>
      <p className="interface-node-case-detail-meta">
        {[caseGroupKey(item) === "success" ? copyText(payload, "successCaseLabel", "成功") : copyText(payload, "failureCaseLabel", "失败"), item.caseType || "case", outcomeLabel(item), `${copyText(payload, "latestDurationLabel", "最近耗时")} ${duration(runElapsedMs(item.latestRun))}`, item.latestRun?.failureReason || "", item.scenario || "", item.requiredForAdmission ? "required_for_admission" : "optional", item.blocked ? copyText(payload, "blockedCaseLabel", "暂不可运行") : ""].filter(Boolean).join(" · ")}
      </p>
      <Summary rows={[
        ["case", item.caseType || "case"],
        ["required", item.requiredForAdmission ? "yes" : "no"],
        ["latest run", run.runId ? tail(run.runId) : "no run"],
        ["elapsed", duration(runElapsedMs(run))],
      ]} />
      <CaseTopology run={run} payload={payload} />
      {run.runId ? (
        <div className="interface-node-case-dependencies">
          <span>{copyText(payload, "cachedRequestLabel", "缓存请求")}</span>
          <div className="interface-node-case-dependency">
            <strong>{[run.workflowId, run.requestSummary?.stepId].filter(Boolean).join(" · ") || "latest request"}</strong>
            <code>{[run.caseRunId, run.requestSummary?.method, run.requestSummary?.full_url || run.requestSummary?.path || run.requestSummary?.uri].filter(Boolean).join(" · ") || "-"}</code>
            <pre>{prettyJSON(run.requestSummary || {})}</pre>
          </div>
        </div>
      ) : null}
      {item.latestRun?.runId ? <a className="button-link interface-node-evidence-link" href={`/evidence-viewer.html?${new URLSearchParams({ caseRun: item.latestRun.runId, caseId: item.id }).toString()}`}>{copyText(payload, "viewEvidenceLink", "查看运行证据")}</a> : null}
      <div className="interface-node-case-actions">
        <button className="button-link interface-node-case-run-button" type="button" disabled={Boolean(item.blocked)} onClick={() => onRunCase(item.id)}>
          {item.blocked ? copyText(payload, "waitPrerequisiteButton", "等待前置数据") : copyText(payload, "runCaseButton", "运行此用例")}
        </button>
      </div>
      <Dependencies item={item} payload={payload} />
    </article>
  );
}

function CasesPanel({ payload, onRunCase, onRunAll }) {
  const cases = payload.cases || [];
  const latestCaseID = cases.find((item) => item.latestRun?.runId && item.latestRun.runId === payload.history?.latestRunId)?.id || cases.find((item) => item.latestRun?.topology)?.id || "";
  const preferredID = contextualCaseID(cases, payload.context) || latestCaseID;
  const [selectedID, setSelectedID] = useState(preferredID || cases[0]?.id || "");
  useEffect(() => {
    if (!cases.length) return;
    if (preferredID && selectedID !== preferredID) {
      setSelectedID(preferredID);
      return;
    }
    if (!cases.some((item) => item.id === selectedID)) {
      setSelectedID(cases[0].id || "");
    }
  }, [cases, preferredID, selectedID]);
  const selected = cases.find((item) => item.id === selectedID) || cases[0] || null;
  const groups = [
    { key: "success", title: copyText(payload, "successCasesTitle", "成功用例"), items: cases.filter((item) => caseGroupKey(item) === "success") },
    { key: "failure", title: copyText(payload, "failureCasesTitle", "失败用例"), items: cases.filter((item) => caseGroupKey(item) === "failure") },
  ];

  return (
    <Panel title={copyText(payload, "casesTitle", "测试用例")} subtitle={payload.context?.runId || payload.context?.flowId ? `${copyText(payload, "casesContextPrefix", "当前 flow")}: ${payload.context.flowId || "-"}${payload.context.runId ? ` · ${tail(payload.context.runId, 18)}` : ""}${payload.context.stepId ? ` · ${payload.context.stepId}` : ""}` : copyText(payload, "casesSubtitle", "接口准入用例与最近运行耗时")} className="interface-node-cases-panel">
      {cases.length ? (
        <>
          <div className="interface-node-case-toolbar">
            <span className="interface-node-case-total">{copyText(payload, "totalElapsedLabel", "最近总耗时")} {duration(cases.reduce((sum, item) => sum + runElapsedMs(item.latestRun), 0))}</span>
            <button type="button" className="button-link interface-node-case-run-all" onClick={() => onRunAll(cases)}>{copyText(payload, "runAllButton", "全部运行")}</button>
          </div>
          <div className="interface-node-case-browser">
            <div className="interface-node-case-list">
              {groups.map((group) => (
                <section className="interface-node-case-group" data-case-group={group.key} key={group.key}>
                  <div className="interface-node-case-group-head">
                    <strong>{group.title}</strong>
                    <span>{group.items.length}</span>
                  </div>
                  {group.items.length ? group.items.map((item) => (
                    <button className={`interface-node-case-list-item ${item.id === selectedID ? "selected" : ""}`} type="button" data-case-id={item.id || ""} onClick={() => setSelectedID(item.id || "")} key={item.id}>
                      <span className="interface-node-case-number">{caseNumber(cases, item)}</span>
                      <strong>{item.title || item.id || "case"}</strong>
                      <span>{[item.id, `${copyText(payload, "elapsedLabel", "耗时")} ${duration(runElapsedMs(item.latestRun))}`, outcomeLabel(item), item.requiredForAdmission ? "required" : "optional", item.blocked ? copyText(payload, "blockedCaseLabel", "暂不可运行") : "", item.scenario || ""].filter(Boolean).join(" · ")}</span>
                      <RunBadge item={item} />
                    </button>
                  )) : <p className="dashboard-empty">{copyText(payload, "caseGroupEmpty", "暂无")}</p>}
                </section>
              ))}
            </div>
            <div className="interface-node-case-detail-wrap">
              <CaseDetail item={selected} cases={cases} onRunCase={onRunCase} payload={payload} />
            </div>
          </div>
        </>
      ) : <p className="dashboard-empty">{copyText(payload, "emptyCases", "当前接口节点还没有配置测试用例。")}</p>}
    </Panel>
  );
}

function AdmissionPanel({ admission, payload }) {
  const blockers = admission.blockers || [];
  if (!blockers.length) return null;
  return (
    <Panel title={copyText(payload, "admissionTitle", "准入阻塞")} subtitle={copyText(payload, "admissionSubtitle", "required_for_admission Case 的当前阻塞项")} className="interface-node-admission">
      <div className="interface-node-admission-blockers">
        {blockers.map((blocker) => (
          <article className="interface-node-admission-blocker" key={blocker.caseId || blocker.title}>
            <div>
              <strong>{blocker.title || blocker.caseId || "required case"}</strong>
              <span className={`react-pill ${blocker.status === "failed" ? "bad" : "warn"}`}>{blocker.status || "blocked"}</span>
            </div>
            <code>{[blocker.caseId, blocker.runId, blocker.failureKind].filter(Boolean).join(" · ") || "-"}</code>
            <p>{blocker.failureReason || "required case is not admitted"}</p>
            {blocker.evidenceHref ? <a className="button-link interface-node-admission-blocker-link" href={blocker.evidenceHref}>{copyText(payload, "openEvidenceLink", "打开证据")}</a> : null}
          </article>
        ))}
      </div>
    </Panel>
  );
}

function HistoryPanel({ payload }) {
  const history = payload.history || {};
  const perCase = Array.isArray(history.perCase) ? history.perCase : [];
  return (
    <Panel title={copyText(payload, "historyTitle", "运行历史")} subtitle={copyText(payload, "historySubtitle", "来自 interface_node_case_run 的最近运行聚合")} className="interface-node-history-panel">
      <div className="interface-node-history-grid">
        <Stat label={copyText(payload, "latestRunStat", "最近运行")} value={tail(history.latestRunId || "-")} title={history.latestRunId || "-"} />
        <Stat label={copyText(payload, "passFailStat", "通过/失败")} value={`${history.passCount || 0}/${history.failCount || 0}`} />
        <Stat label={copyText(payload, "runCountStat", "运行总数")} value={history.runCount || 0} />
        <Stat label={copyText(payload, "latestFailureStat", "最近失败")} value={text(history.latestFailureReason || "-", "-")} title={history.latestFailureReason || "-"} />
        <Stat label={copyText(payload, "totalElapsedStat", "累计耗时")} value={duration(history.totalElapsedMs || 0)} />
      </div>
      <div className="interface-node-history-case-list">
        {perCase.length ? perCase.slice(0, 8).map((item) => (
          <div className="interface-node-history-case" key={item.caseId}>
            <strong>{item.caseId || "-"}</strong>
            <span>{[`${item.passCount || 0}/${item.failCount || 0}`, item.latestStatus || "-", duration(item.latestElapsedMs || 0), item.latestFailureReason || ""].filter(Boolean).join(" · ")}</span>
          </div>
        )) : <p className="dashboard-empty">{copyText(payload, "historyEmpty", "还没有接口级运行历史。")}</p>}
      </div>
    </Panel>
  );
}

function RunsPanel({ payload }) {
  const runs = payload.runs || [];
  return (
    <Panel title={copyText(payload, "runsTitle", "运行证据索引")} subtitle={copyText(payload, "runsSubtitle", "只保留 Evidence 路径和摘要索引，证据正文仍在 Case bundle 中")} className="interface-node-runs-panel">
      <div className="interface-node-run-list">
        {runs.length ? runs.slice(0, 8).map((run) => (
          <a className="environment-node-peer interface-node-run-item" href={run?.runId ? `/evidence-viewer.html?${new URLSearchParams({ caseRun: run.runId, caseId: run.caseId || "" }).toString()}` : "#"} key={run.runId || run.caseId}>
            <strong>{run?.runId || "-"}</strong>
            <span>{`${run?.caseId || "-"} · ${run?.status || "-"}`}</span>
          </a>
        )) : <p className="dashboard-empty">{copyText(payload, "runsEmpty", "还没有接口级 Case run 证据。")}</p>}
      </div>
    </Panel>
  );
}

function FieldCard({ field }) {
  return (
    <article className="interface-node-field-card">
      <strong>{field.displayName || field.fieldPath || field.id}</strong>
      <code>{field.fieldPath || "-"}</code>
      <span>{[field.dataType || "unknown", field.required ? "required" : "optional", field.bindable ? "bindable" : ""].filter(Boolean).join(" · ")}</span>
    </article>
  );
}

function FieldsPanel({ payload, direction, title, subtitle }) {
  const fields = payload.fields?.[direction] || [];
  return (
    <Panel title={title} subtitle={subtitle} className={`interface-node-${direction}-fields`}>
      <div className="interface-node-field-grid">
        {fields.length ? fields.map((field) => <FieldCard field={field} key={field.id || field.fieldPath} />) : <p className="dashboard-empty">{copyText(payload, "fieldsEmpty", "当前接口节点还没有配置字段。")}</p>}
      </div>
    </Panel>
  );
}

function FieldContract({ payload }) {
  const requestFields = payload.fields?.request || [];
  const responseFields = payload.fields?.response || [];
  const rows = [
    ["request required", `${requestFields.filter((field) => field.required).length}/${requestFields.length}`],
    ["response required", `${responseFields.filter((field) => field.required).length}/${responseFields.length}`],
    ["bindable response", responseFields.filter((field) => field.bindable).length],
  ];
  return (
    <Panel title={copyText(payload, "fieldContractTitle", "字段契约")} subtitle={copyText(payload, "fieldContractSubtitle", "只汇总已登记字段配置，不从业务样例推断字段")} className="interface-node-field-contract">
      <div className="interface-node-field-contract-grid">
        {rows.map(([label, value]) => (
          <div key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function MissingNode({ payload, requested }) {
  const available = payload.available || [];
  return (
    <section className="interface-node-layout" aria-label="接口节点测试用例">
      <Panel title={copyText(payload, "missingOptionsTitle", "可选接口节点")} subtitle={copyText(payload, "missingOptionsSubtitle", "当前 active Store 中已登记的接口节点")} className="interface-node-missing-panel">
        <div className="environment-node-peer-list">
          {available.length ? available.map((item) => (
            <a className="environment-node-peer" href={item.href} key={item.id}>
              <strong>{item.displayName || item.id}</strong>
              <span>{`${item.serviceId || "-"} · ${item.operation || "-"}`}</span>
            </a>
          )) : <p className="dashboard-empty">{requested ? copyText(payload, "missingNoNodes", "还没有登记接口节点配置。") : copyText(payload, "missingNoID", "缺少 id 参数。")}</p>}
        </div>
      </Panel>
    </section>
  );
}

function InterfaceNodeApp() {
  const mode = pageMode();
  const [payload, setPayload] = useState(null);
  const [message, setMessage] = useState("loading");
  const requestedID = queryParam("id");

  async function getJSON(path) {
    const response = await fetch(path, { headers: { Accept: "application/json" } });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || data.ok === false) {
      const error = new Error(data.error || response.statusText);
      error.payload = data;
      throw error;
    }
    return data;
  }

  async function load(options = {}) {
    if (!options.silent) setMessage("refreshing...");
    try {
      const params = queryContextParams();
      if (!params.get("id")) params.set("id", requestedID);
      setPayload(await getJSON(`/api/interface-node?${params.toString()}`));
      if (!options.preserveStatus) setMessage("ready");
    } catch (error) {
      setPayload(error.payload || { ok: false, requested: requestedID, available: [] });
      setMessage("missing");
    }
  }

  async function postJSON(path, body) {
    const response = await fetch(path, {
      method: "POST",
      headers: { "content-type": "application/json", Accept: "application/json" },
      body: JSON.stringify(body || {}),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || data.ok === false) throw new Error(data.error || data.stderr || response.statusText);
    return data;
  }

  async function runCase(caseId) {
    if (!caseId) return;
    setMessage(`running ${caseId}`);
    try {
      const result = await postJSON("/api/test-kit/run", { caseId, skipTraceTopology: false, timeoutSeconds: 90 });
      const finalStatus = `${result.ok ? "case run passed" : "case run failed"} · ${duration(result.elapsedMs || 0)}`;
      setMessage(finalStatus);
      await load({ silent: true, preserveStatus: true });
      setMessage(finalStatus);
    } catch (error) {
      setMessage(error.message);
    }
  }

  async function runAll(cases) {
    const runnable = (cases || []).filter((item) => item.id && !item.blocked);
    if (!runnable.length) return;
    setMessage(`running ${runnable.length} cases concurrently`);
    try {
      const result = await postJSON("/api/test-kit/run-batch", {
        caseIds: runnable.map((item) => item.id),
        skipTraceTopology: false,
        timeoutSeconds: 90,
        concurrency: runnable.length,
      });
      const summary = result.summary || {};
      const finalStatus = `all cases finished · ${summary.passed || 0}/${summary.caseCount || runnable.length} passed · ${duration(result.elapsedMs || 0)}`;
      setMessage(finalStatus);
      await load({ silent: true, preserveStatus: true });
      setMessage(finalStatus);
    } catch (error) {
      setMessage(error.message);
    }
  }

  useEffect(() => {
    load();
  }, []);

  const node = payload?.node || {};
  const admission = payload?.admission || {};
  const nodeID = node.id || requestedID;
  const modeParams = (targetMode) => {
    const params = queryContextParams();
    if (nodeID) params.set("id", nodeID);
    const file = targetMode === "history" ? "interface-node-history.html" : targetMode === "fields" ? "interface-node-fields.html" : "interface-node.html";
    return `/${file}?${params.toString()}`;
  };
  const stats = useMemo(() => [
    [copyText(payload, "admissionStat", "准入"), admission.status || "pending"],
    [copyText(payload, "nodeStatusStat", "节点状态"), node.status || "draft"],
    [copyText(payload, "versionStat", "版本"), node.version || "-"],
    [copyText(payload, "requiredCasesStat", "必需 Case"), admission.requiredCaseCount ?? 0],
    [copyText(payload, "passedCasesStat", "已通过"), admission.passedCaseCount ?? 0],
    [copyText(payload, "latestRunStat", "最新运行"), tail(admission.latestRunId || "-"), admission.latestRunId || "-"],
  ], [admission, node.status, node.version, payload?.presentation?.copy]);
  const missing = payload?.ok === false || message === "missing";
  const pageClass = mode === "history" ? "interface-node-history-page" : mode === "fields" ? "interface-node-field-page" : "interface-node-main-page";
  const pageTitle = missing
    ? copyText(payload, "missingPageTitle", "未找到接口节点")
    : mode === "history"
      ? copyText(payload, "historyPageTitle", "接口节点运行历史")
      : mode === "fields"
        ? copyText(payload, "fieldsPageTitle", "接口节点字段契约")
        : node.displayName || node.id || copyText(payload, "mainPageTitle", "接口节点");
  const contentClass = mode === "history" ? "interface-node-layout interface-node-history-layout" : mode === "fields" ? "interface-node-layout interface-node-field-layout" : "interface-node-layout";
  const workflowCaseHref = workflowCaseSetHref();

  return (
    <main className={`app interface-node-page ${pageClass}`} data-template-id={mode === "history" ? "TPL-INTERFACE-NODE-RUN-HISTORY-V1" : mode === "fields" ? "TPL-INTERFACE-NODE-FIELD-CONTRACT-V1" : "TPL-INTERFACE-NODE-CASE-LIST-V1"} data-interface-node-mode={mode}>
      <div className="template-watermark" aria-label="模板编号">{mode === "history" ? "TPL-INTERFACE-NODE-RUN-HISTORY-V1" : mode === "fields" ? "TPL-INTERFACE-NODE-FIELD-CONTRACT-V1" : "TPL-INTERFACE-NODE-CASE-LIST-V1"}</div>
      <section className="topbar interface-node-topbar">
        <div>
          <p className="viewer-eyebrow">{mode === "history" ? "Interface Node History" : mode === "fields" ? "Interface Node Fields" : "Interface Node"}</p>
          <h1>{pageTitle}</h1>
          <p>{missing ? payload?.requested || requestedID || "缺少 id" : [node.serviceId, node.operation, `${text(node.method)} ${text(node.path)}`, ...(node.tags || [])].filter(Boolean).join(" · ")}</p>
        </div>
        <div className="dashboard-top-stats" aria-label="接口节点摘要">
          {missing ? (
            <>
              <Stat label={copyText(payload, "nodeStatusStat", "状态")} value="missing" />
              <Stat label={copyText(payload, "availableNodesStat", "可选节点")} value={(payload?.available || []).length} />
            </>
          ) : stats.map(([label, value, title]) => <Stat label={label} value={value} title={title} key={label} />)}
        </div>
        <div className="actions">
          <span className="environment-status-pill" role="status">{message}</span>
          {workflowCaseHref ? <a className="button-link" href={workflowCaseHref}>Workflow case set</a> : null}
          <a className="button-link" href={node.serviceId ? `/environment-node.html?id=${encodeURIComponent(node.serviceId)}` : "/environment-nodes.html"}>{node.serviceId ? copyText(payload, "serviceNodeLink", "服务节点") : copyText(payload, "environmentNodesLink", "环境节点")}</a>
          <a className={`button-link ${mode === "main" ? "disabled-link" : ""}`} href={nodeID ? modeParams("main") : "/interface-node.html"}>{copyText(payload, "mainNav", "用例概览")}</a>
          <a className={`button-link ${mode === "history" ? "disabled-link" : ""}`} href={nodeID ? modeParams("history") : "/interface-node-history.html"}>{copyText(payload, "historyNav", "运行历史")}</a>
          <a className={`button-link ${mode === "fields" ? "disabled-link" : ""}`} href={nodeID ? modeParams("fields") : "/interface-node-fields.html"}>{copyText(payload, "fieldsNav", "字段契约")}</a>
          <button type="button" title={copyText(payload, "refreshTitle", "刷新状态")} onClick={() => load()}>
            <RefreshCw size={15} aria-hidden="true" />
          </button>
        </div>
      </section>

      {missing ? <MissingNode payload={payload || {}} requested={requestedID} /> : (
        <section className={contentClass} aria-label="接口节点测试用例">
          {mode === "history" ? (
            <>
              <HistoryPanel payload={payload || {}} />
              <RunsPanel payload={payload || {}} />
            </>
          ) : mode === "fields" ? (
            <>
              <FieldContract payload={payload || {}} />
              <FieldsPanel payload={payload || {}} direction="request" title={copyText(payload, "requestFieldsTitle", "标准请求参数")} subtitle={copyText(payload, "requestFieldsSubtitle", "接口入参字段，可用于后续模板确认")} />
              <FieldsPanel payload={payload || {}} direction="response" title={copyText(payload, "responseFieldsTitle", "标准返回参数")} subtitle={copyText(payload, "responseFieldsSubtitle", "可连线字段应在配置中标记为 bindable")} />
            </>
          ) : (
            <>
              <AttentionPanel attention={payload?.attention} payload={payload || {}} />
              <RequestTemplatePanel payload={payload || {}} />
              <CasesPanel payload={payload || {}} onRunCase={runCase} onRunAll={runAll} />
              <AdmissionPanel admission={admission} payload={payload || {}} />
            </>
          )}
        </section>
      )}
    </main>
  );
}

createRoot(document.getElementById("react-interface-node-root")).render(<InterfaceNodeApp />);
