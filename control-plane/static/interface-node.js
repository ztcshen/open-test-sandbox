const interfaceNodeEl = (id) => document.getElementById(id);
let selectedInterfaceNodeCaseId = "";

async function interfaceNodeRequest(path) {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    const error = new Error(body.error || response.statusText);
    error.payload = body;
    throw error;
  }
  return body;
}

async function interfaceNodePost(path, payload) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "content-type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload || {}),
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    const error = new Error(body.error || body.stderr || response.statusText);
    error.payload = body;
    throw error;
  }
  return body;
}

function requestedInterfaceNodeId() {
  return new URLSearchParams(window.location.search).get("id") || "";
}

function interfaceNodePageMode() {
  return document.querySelector(".interface-node-page")?.dataset.interfaceNodeMode || "main";
}

function setInterfaceNodeStatus(text) {
  interfaceNodeEl("interfaceNodeStatus").textContent = text;
}

function interfaceNodeText(value, fallback = "-") {
  const text = String(value ?? "").trim();
  return text || fallback;
}

function interfaceNodeTailText(value, tailLength = 12) {
  const text = interfaceNodeText(value);
  if (text.length <= tailLength) {
    return text;
  }
  return `...${text.slice(-tailLength)}`;
}

function interfaceNodeStat(label, value, title = "") {
  const box = document.createElement("div");
  const span = document.createElement("span");
  const strong = document.createElement("strong");
  span.textContent = label;
  strong.textContent = interfaceNodeText(value);
  box.title = interfaceNodeText(title || value);
  box.appendChild(span);
  box.appendChild(strong);
  return box;
}

function setInterfaceNodeStats(stats) {
  const target = interfaceNodeEl("interfaceNodeStats");
  target.innerHTML = "";
  stats.forEach(([label, value, title]) => target.appendChild(interfaceNodeStat(label, value, title)));
}

function interfaceNodePanel(title, subtitle, className = "") {
  const panel = document.createElement("section");
  panel.className = `environment-node-detail-panel interface-node-panel ${className}`.trim();
  const head = document.createElement("div");
  head.className = "dashboard-section-head";
  const h2 = document.createElement("h2");
  h2.textContent = title;
  const p = document.createElement("p");
  p.textContent = subtitle;
  head.appendChild(h2);
  head.appendChild(p);
  panel.appendChild(head);
  return panel;
}

function interfaceNodeDetails(rows) {
  const list = document.createElement("dl");
  list.className = "environment-node-detail-list interface-node-detail-list";
  rows.forEach(([label, value]) => {
    const dt = document.createElement("dt");
    const dd = document.createElement("dd");
    dt.textContent = label;
    dd.textContent = interfaceNodeText(value);
    list.appendChild(dt);
    list.appendChild(dd);
  });
  return list;
}

function renderInterfaceNodeAdmissionBlockers(admission) {
  const blockers = admission.blockers || [];
  const wrap = document.createElement("div");
  wrap.className = "interface-node-admission-blockers";
  if (!blockers.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "当前没有准入阻塞项。";
    wrap.appendChild(empty);
    return wrap;
  }
  blockers.forEach((blocker) => {
    const item = document.createElement("article");
    item.className = "interface-node-admission-blocker";
    const top = document.createElement("div");
    const title = document.createElement("strong");
    title.textContent = blocker.title || blocker.caseId || "required case";
    const badge = document.createElement("span");
    badge.className = `react-pill ${blocker.status === "failed" ? "bad" : "warn"}`;
    badge.textContent = blocker.status || "blocked";
    top.appendChild(title);
    top.appendChild(badge);
    const meta = document.createElement("code");
    meta.textContent = [blocker.caseId, blocker.runId, blocker.failureKind].filter(Boolean).join(" · ") || "-";
    const reason = document.createElement("p");
    reason.textContent = blocker.failureReason || "required case is not admitted";
    item.appendChild(top);
    item.appendChild(meta);
    item.appendChild(reason);
    if (blocker.evidenceHref) {
      const link = document.createElement("a");
      link.className = "button-link interface-node-admission-blocker-link";
      link.href = blocker.evidenceHref;
      link.textContent = "打开证据";
      item.appendChild(link);
    }
    wrap.appendChild(item);
  });
  return wrap;
}

function interfaceNodePrettyJSON(raw) {
  if (window.InterfaceRunTemplate) {
    return window.InterfaceRunTemplate.prettyJSON(raw);
  }
  if (!raw) {
    return "{}";
  }
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return String(raw);
  }
}

function renderInterfaceNodeCaseDependencies(item) {
  const dependencies = item.dependencies || [];
  if (!dependencies.length) {
    return null;
  }
  const wrap = document.createElement("div");
  wrap.className = "interface-node-case-dependencies";
  const label = document.createElement("span");
  label.textContent = "前置数据";
  wrap.appendChild(label);
  dependencies.forEach((dependency) => {
    const card = document.createElement("div");
    card.className = "interface-node-case-dependency";
    const title = document.createElement("strong");
    title.textContent = dependency.profile?.name || dependency.fixtureProfileId || dependency.id;
    const meta = document.createElement("code");
    const tables = (dependency.tableBindings || []).map((binding) => `${binding.schemaName}.${binding.tableName}`).join(", ");
    meta.textContent = [
      dependency.fixtureProfileId,
      dependency.required ? "required" : "optional",
      tables,
    ].filter(Boolean).join(" · ");
    const mappings = document.createElement("pre");
    mappings.textContent = interfaceNodePrettyJSON(dependency.mappingsJson || "[]");
    card.appendChild(title);
    card.appendChild(meta);
    card.appendChild(mappings);
    wrap.appendChild(card);
  });
  return wrap;
}

function interfaceNodeFieldCard(field) {
  const card = document.createElement("article");
  card.className = "interface-node-field-card";
  const title = document.createElement("strong");
  title.textContent = field.displayName || field.fieldPath || field.id;
  const path = document.createElement("code");
  path.textContent = field.fieldPath || "-";
  const meta = document.createElement("span");
  meta.textContent = [
    field.dataType || "unknown",
    field.required ? "required" : "optional",
    field.bindable ? "bindable" : "",
  ].filter(Boolean).join(" · ");
  card.appendChild(title);
  card.appendChild(path);
  card.appendChild(meta);
  return card;
}

function renderInterfaceNodeFields(payload, direction, title, subtitle) {
  const panel = interfaceNodePanel(title, subtitle, `interface-node-${direction}-fields`);
  const list = document.createElement("div");
  list.className = "interface-node-field-grid";
  const fields = payload.fields?.[direction] || [];
  if (!fields.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "当前接口节点还没有配置字段。";
    list.appendChild(empty);
  } else {
    fields.forEach((field) => list.appendChild(interfaceNodeFieldCard(field)));
  }
  panel.appendChild(list);
  return panel;
}

function renderInterfaceNodeFieldContract(payload) {
  const requestFields = payload.fields?.request || [];
  const responseFields = payload.fields?.response || [];
  const requiredRequest = requestFields.filter((field) => field.required).length;
  const requiredResponse = responseFields.filter((field) => field.required).length;
  const bindableResponse = responseFields.filter((field) => field.bindable).length;
  const panel = interfaceNodePanel("字段契约", "只汇总已登记字段配置，不从业务样例推断字段", "interface-node-field-contract");
  const grid = document.createElement("div");
  grid.className = "interface-node-field-contract-grid";
  [
    ["request required", `${requiredRequest}/${requestFields.length}`],
    ["response required", `${requiredResponse}/${responseFields.length}`],
    ["bindable response", bindableResponse],
  ].forEach(([label, value]) => {
    const item = document.createElement("div");
    const span = document.createElement("span");
    span.textContent = label;
    const strong = document.createElement("strong");
    strong.textContent = String(value);
    item.appendChild(span);
    item.appendChild(strong);
    grid.appendChild(item);
  });
  panel.appendChild(grid);
  return panel;
}

function interfaceNodeTemplateSummary(template) {
  return [
    template.id || "",
    template.version || "",
    template.status || "",
  ].filter(Boolean).join(" · ");
}

function renderInterfaceNodeRequestTemplateCard(template) {
  const card = document.createElement("article");
  card.className = "interface-node-request-template-card";
  const top = document.createElement("div");
  top.className = "interface-node-request-template-card-top";
  const title = document.createElement("strong");
  title.textContent = template.name || template.id || "公共请求模板";
  const code = document.createElement("code");
  code.textContent = interfaceNodeTemplateSummary(template) || "-";
  top.appendChild(title);
  top.appendChild(code);
  const pre = document.createElement("pre");
  pre.textContent = interfaceNodePrettyJSON(template.templateJson || template.template_json || "{}");
  card.appendChild(top);
  card.appendChild(pre);
  return card;
}

function renderInterfaceNodeRequestTemplateField(field) {
  const row = document.createElement("div");
  row.className = "interface-node-request-template-field";
  const title = document.createElement("strong");
  title.textContent = field.displayName || field.fieldPath || field.id || "-";
  const path = document.createElement("code");
  path.textContent = field.fieldPath || "-";
  const meta = document.createElement("span");
  meta.textContent = [
    field.dataType || "unknown",
    field.required ? "required" : "optional",
    field.bindable ? "bindable" : "",
  ].filter(Boolean).join(" · ");
  row.appendChild(title);
  row.appendChild(path);
  row.appendChild(meta);
  return row;
}

function renderInterfaceNodeRequestTemplates(payload) {
  const templates = Array.isArray(payload.requestTemplates) ? payload.requestTemplates : [];
  const requestFields = payload.fields?.request || [];
  const panel = interfaceNodePanel(
    "公共模板参数",
    templates.length
      ? "来自 interface_node_request_template，Case 只维护差异 Patch"
      : "尚未登记公共请求模板，先按接口字段契约展示公共参数骨架",
    "interface-node-request-template-panel",
  );
  const body = document.createElement("div");
  body.className = "interface-node-request-template-body";
  if (!templates.length) {
    body.classList.add("no-template");
  }

  const fields = document.createElement("div");
  fields.className = "interface-node-request-template-fields";
  const fieldTitle = document.createElement("span");
  fieldTitle.textContent = "公共参数";
  fields.appendChild(fieldTitle);
  if (!requestFields.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "当前接口节点还没有登记请求字段。";
    fields.appendChild(empty);
  } else {
    requestFields.forEach((field) => fields.appendChild(renderInterfaceNodeRequestTemplateField(field)));
  }

  const templateList = document.createElement("div");
  templateList.className = "interface-node-request-template-list";
  const templateTitle = document.createElement("span");
  templateTitle.textContent = "模板 JSON";
  templateList.appendChild(templateTitle);
  if (!templates.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "未找到 interface_node_request_template 记录。新增必填字段时，应优先补公共请求模板，再让 Case Patch 表达差异。";
    templateList.appendChild(empty);
  } else {
    templates.forEach((template) => templateList.appendChild(renderInterfaceNodeRequestTemplateCard(template)));
  }

  body.appendChild(fields);
  body.appendChild(templateList);
  panel.appendChild(body);
  return panel;
}

function interfaceNodeCaseIsNegative(item) {
  return interfaceNodeCaseGroupKey(item) === "failure";
}

function interfaceNodeRunOutcomeLabel(item) {
  const run = item?.latestRun;
  if (!run) return "no run";
  const status = String(run.status || "").trim().toLowerCase();
  const passed = status === "pass" || status === "passed" || status === "success" || status === "succeeded";
  if (interfaceNodeCaseIsNegative(item)) {
    return passed ? "命中预期失败" : "未命中预期失败";
  }
  return run.status || "unknown";
}

function interfaceNodeRunBadge(item) {
  const run = item?.latestRun;
  const badge = document.createElement("span");
  badge.className = `react-pill ${run?.status === "pass" || run?.status === "passed" ? "good" : run ? "bad" : "warn"}`;
  badge.textContent = interfaceNodeRunOutcomeLabel(item);
  return badge;
}

function interfaceNodeFormatDuration(ms) {
  const value = Number(ms || 0);
  if (!Number.isFinite(value) || value <= 0) {
    return "-";
  }
  if (value < 1000) {
    return `${Math.round(value)}ms`;
  }
  return `${(value / 1000).toFixed(1)}s`;
}

function interfaceNodeRunElapsedMs(run) {
  if (!run) return 0;
  const direct = Number(run.elapsedMs || 0);
  if (Number.isFinite(direct) && direct > 0) {
    return direct;
  }
  try {
    const summary = JSON.parse(run.summaryJson || "{}");
    const parsed = Number(summary.elapsedMs || summary.elapsed_ms || 0);
    return Number.isFinite(parsed) ? parsed : 0;
  } catch {
    return 0;
  }
}

function interfaceNodeTotalElapsedMs(cases) {
  return (cases || []).reduce((sum, item) => sum + interfaceNodeRunElapsedMs(item.latestRun), 0);
}

function interfaceNodeCaseGroupKey(item) {
  const type = String(item?.caseType || "").trim().toLowerCase();
  if (type === "success" || type === "pass" || type === "positive") {
    return "success";
  }
  return "failure";
}

function interfaceNodeCaseGroups(cases) {
  return [
    {
      key: "success",
      title: "成功用例",
      items: cases.filter((item) => interfaceNodeCaseGroupKey(item) === "success"),
    },
    {
      key: "failure",
      title: "失败用例",
      items: cases.filter((item) => interfaceNodeCaseGroupKey(item) === "failure"),
    },
  ];
}

function interfaceNodeCaseNumber(cases, item) {
  if (!item) return "";
  const groupKey = interfaceNodeCaseGroupKey(item);
  const prefix = groupKey === "success" ? "S" : "F";
  const items = (cases || []).filter((candidate) => interfaceNodeCaseGroupKey(candidate) === groupKey);
  const index = items.findIndex((candidate) => {
    if (candidate?.id && item?.id) return candidate.id === item.id;
    return candidate === item;
  });
  return `${prefix}${String(Math.max(index, 0) + 1).padStart(2, "0")}`;
}

function selectedInterfaceNodeCase(cases) {
  if (!Array.isArray(cases) || cases.length === 0) {
    selectedInterfaceNodeCaseId = "";
    return null;
  }
  const selected = cases.find((item) => item.id === selectedInterfaceNodeCaseId);
  if (selected) {
    return selected;
  }
  selectedInterfaceNodeCaseId = cases[0].id || "";
  return cases[0];
}

async function runInterfaceNodeCase(caseId, button) {
  if (!caseId) return;
  const originalText = button?.textContent || "运行 Case";
  const started = performance.now();
  let timer = 0;
  if (button) {
    button.disabled = true;
    button.textContent = "运行中";
    timer = window.setInterval(() => {
      button.textContent = `运行中 ${interfaceNodeFormatDuration(performance.now() - started)}`;
    }, 200);
  }
  setInterfaceNodeStatus(`running ${caseId}`);
  try {
    const result = await interfaceNodePost("/api/test-kit/run", {
      caseId,
      dryRun: false,
      skipTraceTopology: false,
      timeoutSeconds: 90,
    });
    const finalStatus = `${result.ok ? "case run passed" : "case run failed"} · ${interfaceNodeFormatDuration(result.elapsedMs || performance.now() - started)}`;
    setInterfaceNodeStatus(finalStatus);
    await loadInterfaceNodeDetail();
    setInterfaceNodeStatus(finalStatus);
  } catch (error) {
    setInterfaceNodeStatus(error.message);
  } finally {
    if (timer) {
      window.clearInterval(timer);
    }
    if (button) {
      button.disabled = false;
      button.textContent = originalText;
    }
  }
}

function refreshInterfaceNodeDetailInBackground(finalStatus) {
  window.setTimeout(() => {
    loadInterfaceNodeDetail({ silent: true, preserveStatus: true }).catch((error) => {
      setInterfaceNodeStatus(`${finalStatus} · refresh failed: ${error.message}`);
    });
  }, 0);
}

function interfaceNodeCaseBlockedText(item) {
  if (!item?.blocked) return "";
  if (item.blockedReason === "requires_fixture_instance") {
    return "等待前置数据";
  }
  return "暂不可运行";
}

async function runAllInterfaceNodeCases(cases, button, totalLabel) {
  const runnable = (cases || []).filter((item) => item.id && !item.blocked);
  if (!runnable.length) return;
  const originalText = button?.textContent || "全部运行";
  const started = performance.now();
  let timer = 0;
  if (button) {
    button.disabled = true;
    button.textContent = "运行中";
    timer = window.setInterval(() => {
      const elapsed = interfaceNodeFormatDuration(performance.now() - started);
      button.textContent = `运行中 ${elapsed}`;
      if (totalLabel) totalLabel.textContent = `本次总耗时 ${elapsed}`;
    }, 200);
  }
  try {
    setInterfaceNodeStatus(`running ${runnable.length} cases concurrently`);
    const result = await interfaceNodePost("/api/test-kit/run-batch", {
      caseIds: runnable.map((item) => item.id),
      dryRun: false,
      skipTraceTopology: false,
      timeoutSeconds: 90,
      concurrency: runnable.length,
    });
    const elapsed = interfaceNodeFormatDuration(performance.now() - started);
    const summary = result.summary || {};
    const finalStatus = `all cases finished · ${summary.passed || 0}/${summary.caseCount || runnable.length} passed · ${elapsed}`;
    setInterfaceNodeStatus(finalStatus);
    if (totalLabel) totalLabel.textContent = `本次总耗时 ${interfaceNodeFormatDuration(result.elapsedMs || performance.now() - started)}`;
    refreshInterfaceNodeDetailInBackground(finalStatus);
  } catch (error) {
    setInterfaceNodeStatus(error.message);
  } finally {
    if (timer) {
      window.clearInterval(timer);
    }
    if (button) {
      button.disabled = false;
      button.textContent = originalText;
    }
  }
}

function renderInterfaceNodeCaseListItem(item, payload, detailTarget, placeDetailTarget) {
  const cases = payload.cases || [];
  const button = document.createElement("button");
  button.type = "button";
  button.className = "interface-node-case-list-item";
  button.dataset.caseId = item.id || "";
  if (item.id === selectedInterfaceNodeCaseId) {
    button.classList.add("selected");
  }
  const caseNumber = document.createElement("span");
  caseNumber.className = "interface-node-case-number";
  caseNumber.textContent = interfaceNodeCaseNumber(cases, item);
  const title = document.createElement("strong");
  title.textContent = item.title || item.id || "case";
  const meta = document.createElement("span");
  meta.textContent = [
    item.id,
    `耗时 ${interfaceNodeFormatDuration(interfaceNodeRunElapsedMs(item.latestRun))}`,
    interfaceNodeRunOutcomeLabel(item),
    item.requiredForAdmission ? "required" : "optional",
    interfaceNodeCaseBlockedText(item),
    item.scenario || "",
  ].filter(Boolean).join(" · ");
  button.appendChild(caseNumber);
  button.appendChild(title);
  button.appendChild(meta);
  button.appendChild(interfaceNodeRunBadge(item));
  button.addEventListener("click", () => {
    selectedInterfaceNodeCaseId = item.id || "";
    detailTarget.replaceChildren(renderInterfaceNodeCaseDetail(selectedInterfaceNodeCase(cases), cases));
    placeDetailTarget?.(button);
    const list = button.closest(".interface-node-case-browser");
    list?.querySelectorAll(".interface-node-case-list-item").forEach((node) => {
      node.classList.toggle("selected", node.getAttribute("data-case-id") === selectedInterfaceNodeCaseId);
    });
  });
  return button;
}

function renderInterfaceNodeCaseDetail(item, cases = []) {
  const detail = document.createElement("article");
  detail.className = "interface-node-case-detail";
  if (!item) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "当前接口节点还没有配置测试用例。";
    detail.appendChild(empty);
    return detail;
  }

  const top = document.createElement("div");
  top.className = "interface-node-case-detail-top";
  const title = document.createElement("div");
  const h3 = document.createElement("h3");
  h3.textContent = `${interfaceNodeCaseNumber(cases, item)} · ${item.title || item.id}`;
  const code = document.createElement("code");
  code.textContent = item.id || "-";
  title.appendChild(h3);
  title.appendChild(code);
  top.appendChild(title);
  top.appendChild(interfaceNodeRunBadge(item));
  detail.appendChild(top);

  const meta = document.createElement("p");
  meta.className = "interface-node-case-detail-meta";
  meta.textContent = [
    interfaceNodeCaseGroupKey(item) === "success" ? "成功" : "失败",
    item.caseType || "case",
    interfaceNodeRunOutcomeLabel(item),
    `最近耗时 ${interfaceNodeFormatDuration(interfaceNodeRunElapsedMs(item.latestRun))}`,
    item.latestRun?.failureReason || "",
    item.scenario || "",
    item.requiredForAdmission ? "required_for_admission" : "optional",
    interfaceNodeCaseBlockedText(item),
  ].filter(Boolean).join(" · ");
  detail.appendChild(meta);

  const run = item.latestRun || {};
  if (window.InterfaceRunTemplate) {
    const summary = window.InterfaceRunTemplate.renderSummary([
      ["case", item.caseType || "case"],
      ["required", item.requiredForAdmission ? "yes" : "no"],
      ["latest run", run.runId ? interfaceNodeTailText(run.runId) : "no run"],
      ["elapsed", interfaceNodeFormatDuration(interfaceNodeRunElapsedMs(run))],
    ]);
    summary.classList.add("interface-node-case-run-summary");
    detail.appendChild(summary);
  }

  if (item.latestRun?.runId) {
    const evidence = document.createElement("a");
    evidence.className = "button-link interface-node-evidence-link";
    evidence.href = `/evidence-viewer.html?caseRun=${encodeURIComponent(item.latestRun.runId)}`;
    evidence.textContent = "查看运行证据";
    detail.appendChild(evidence);
  }

  const actions = document.createElement("div");
  actions.className = "interface-node-case-actions";
  const runButton = document.createElement("button");
  runButton.className = "button-link interface-node-case-run-button";
  runButton.type = "button";
  runButton.textContent = item.blocked ? "等待前置数据" : "运行此用例";
  runButton.disabled = Boolean(item.blocked);
  if (!item.blocked) {
    runButton.addEventListener("click", () => runInterfaceNodeCase(item.id, runButton));
  }
  actions.appendChild(runButton);
  detail.appendChild(actions);

  const dependencies = renderInterfaceNodeCaseDependencies(item);
  if (dependencies) {
    detail.appendChild(dependencies);
  }

  return detail;
}

function renderInterfaceNodeCases(payload) {
  const panel = interfaceNodePanel("测试用例", "接口准入用例与最近运行耗时", "interface-node-cases-panel");
  const cases = payload.cases || [];
  selectedInterfaceNodeCase(cases);
  if (!cases.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "当前接口节点还没有配置测试用例。";
    panel.appendChild(empty);
    return panel;
  }

  const toolbar = document.createElement("div");
  toolbar.className = "interface-node-case-toolbar";
  const totalLabel = document.createElement("span");
  totalLabel.className = "interface-node-case-total";
  totalLabel.textContent = `最近总耗时 ${interfaceNodeFormatDuration(interfaceNodeTotalElapsedMs(cases))}`;
  const runAllButton = document.createElement("button");
  runAllButton.type = "button";
  runAllButton.className = "button-link interface-node-case-run-all";
  runAllButton.textContent = "全部运行";
  runAllButton.addEventListener("click", () => runAllInterfaceNodeCases(cases, runAllButton, totalLabel));
  toolbar.appendChild(totalLabel);
  toolbar.appendChild(runAllButton);
  panel.appendChild(toolbar);

  const browser = document.createElement("div");
  browser.className = "interface-node-case-browser";
  const list = document.createElement("div");
  list.className = "interface-node-case-list";
  const detailTarget = document.createElement("div");
  detailTarget.className = "interface-node-case-detail-wrap";
  detailTarget.appendChild(renderInterfaceNodeCaseDetail(selectedInterfaceNodeCase(cases), cases));
  let selectedButton = null;
  const placeDetailTarget = (button) => {
    if (button?.parentElement) {
      button.insertAdjacentElement("afterend", detailTarget);
    }
  };

  interfaceNodeCaseGroups(cases).forEach((group) => {
    const groupEl = document.createElement("section");
    groupEl.className = "interface-node-case-group";
    groupEl.dataset.caseGroup = group.key;
    const head = document.createElement("div");
    head.className = "interface-node-case-group-head";
    const title = document.createElement("strong");
    title.textContent = group.title;
    const count = document.createElement("span");
    count.textContent = String(group.items.length);
    head.appendChild(title);
    head.appendChild(count);
    groupEl.appendChild(head);
    if (!group.items.length) {
      const empty = document.createElement("p");
      empty.className = "dashboard-empty";
      empty.textContent = "暂无";
      groupEl.appendChild(empty);
    } else {
      group.items.forEach((item) => {
        const button = renderInterfaceNodeCaseListItem(item, payload, detailTarget, placeDetailTarget);
        if ((item.id || "") === selectedInterfaceNodeCaseId) {
          selectedButton = button;
        }
        groupEl.appendChild(button);
      });
    }
    list.appendChild(groupEl);
  });

  browser.appendChild(list);
  if (selectedButton) {
    placeDetailTarget(selectedButton);
  } else {
    browser.appendChild(detailTarget);
  }
  panel.appendChild(browser);
  return panel;
}

function renderInterfaceNodeHistory(payload) {
  const history = payload.history || {};
  const panel = interfaceNodePanel("运行历史", "来自 interface_node_case_run 的最近运行聚合", "interface-node-history-panel");
  const grid = document.createElement("div");
  grid.className = "interface-node-history-grid";
  [
    ["最近运行", interfaceNodeTailText(history.latestRunId || "-"), history.latestRunId || "-"],
    ["通过/失败", `${history.passCount || 0}/${history.failCount || 0}`],
    ["运行总数", history.runCount || 0],
    ["最近失败", interfaceNodeText(history.latestFailureReason || "-", "-"), history.latestFailureReason || "-"],
    ["累计耗时", interfaceNodeFormatDuration(history.totalElapsedMs || 0)],
  ].forEach(([label, value, title]) => {
    grid.appendChild(interfaceNodeStat(label, value, title));
  });
  panel.appendChild(grid);

  const perCase = Array.isArray(history.perCase) ? history.perCase : [];
  const list = document.createElement("div");
  list.className = "interface-node-history-case-list";
  perCase.slice(0, 8).forEach((item) => {
    const row = document.createElement("div");
    row.className = "interface-node-history-case";
    const strong = document.createElement("strong");
    strong.textContent = item.caseId || "-";
    const span = document.createElement("span");
    span.textContent = [
      `${item.passCount || 0}/${item.failCount || 0}`,
      item.latestStatus || "-",
      interfaceNodeFormatDuration(item.latestElapsedMs || 0),
      item.latestFailureReason || "",
    ].filter(Boolean).join(" · ");
    row.appendChild(strong);
    row.appendChild(span);
    list.appendChild(row);
  });
  if (!list.children.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "还没有接口级运行历史。";
    list.appendChild(empty);
  }
  panel.appendChild(list);
  return panel;
}

function renderInterfaceNodeRuns(payload) {
  const panel = interfaceNodePanel("运行证据索引", "只保留 Evidence 路径和摘要索引，证据正文仍在 Case bundle 中", "interface-node-runs-panel");
  const list = document.createElement("div");
  list.className = "interface-node-run-list";
  (payload.runs || []).slice(0, 8).forEach((run) => {
    const item = document.createElement("a");
    item.className = "environment-node-peer interface-node-run-item";
    item.href = run?.runId ? `/evidence-viewer.html?caseRun=${encodeURIComponent(run.runId)}` : "#";
    const strong = document.createElement("strong");
    strong.textContent = run?.runId || "-";
    const span = document.createElement("span");
    span.textContent = `${run?.caseId || "-"} · ${run?.status || "-"}`;
    item.appendChild(strong);
    item.appendChild(span);
    list.appendChild(item);
  });
  if (!list.children.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "还没有接口级 Case run 证据。";
    list.appendChild(empty);
  }
  panel.appendChild(list);
  return panel;
}

function renderInterfaceNodeDetail(payload, options = {}) {
  const mode = interfaceNodePageMode();
  const node = payload.node || {};
  const admission = payload.admission || {};
  interfaceNodeEl("interfaceNodeTitle").textContent = node.displayName || node.id || "接口节点";
  interfaceNodeEl("interfaceNodeSummary").textContent = `${interfaceNodeText(node.serviceId)} · ${interfaceNodeText(node.operation)} · ${interfaceNodeText(node.method)} ${interfaceNodeText(node.path)}`;
  const serviceLink = interfaceNodeEl("interfaceNodeServiceLink");
  serviceLink.href = node.serviceId ? `/environment-node.html?id=${encodeURIComponent(node.serviceId)}` : "/environment-nodes.html";
  serviceLink.textContent = node.serviceId ? "服务节点" : "环境节点";
  const nodeId = node.id || requestedInterfaceNodeId();
  const mainLink = interfaceNodeEl("interfaceNodeMainLink");
  const historyLink = interfaceNodeEl("interfaceNodeHistoryLink");
  const fieldsLink = interfaceNodeEl("interfaceNodeFieldsLink");
  if (mainLink) {
    mainLink.href = nodeId ? `/interface-node.html?id=${encodeURIComponent(nodeId)}` : "/interface-node.html";
    mainLink.classList.toggle("disabled-link", mode === "main");
  }
  if (historyLink) {
    historyLink.href = nodeId ? `/interface-node-history.html?id=${encodeURIComponent(nodeId)}` : "/interface-node-history.html";
    historyLink.classList.toggle("disabled-link", mode === "history");
  }
  if (fieldsLink) {
    fieldsLink.href = nodeId ? `/interface-node-fields.html?id=${encodeURIComponent(nodeId)}` : "/interface-node-fields.html";
    fieldsLink.classList.toggle("disabled-link", mode === "fields");
  }
  setInterfaceNodeStats([
    ["准入", admission.status || "pending"],
    ["必需 Case", admission.requiredCaseCount ?? 0],
    ["已通过", admission.passedCaseCount ?? 0],
    ["最新运行", interfaceNodeTailText(admission.latestRunId || "-"), admission.latestRunId || "-"],
  ]);

  const content = interfaceNodeEl("interfaceNodeContent");
  content.innerHTML = "";
  if (mode === "history") {
    content.appendChild(renderInterfaceNodeHistory(payload));
    content.appendChild(renderInterfaceNodeRuns(payload));
  } else if (mode === "fields") {
    content.appendChild(renderInterfaceNodeFieldContract(payload));
    content.appendChild(renderInterfaceNodeFields(payload, "request", "标准请求参数", "接口入参字段，可用于后续模板确认"));
    content.appendChild(renderInterfaceNodeFields(payload, "response", "标准返回参数", "可连线字段应在配置中标记为 bindable"));
  } else {
    content.appendChild(renderInterfaceNodeRequestTemplates(payload));
    content.appendChild(renderInterfaceNodeCases(payload));
    if ((admission.blockers || []).length) {
      const admissionPanel = interfaceNodePanel("准入阻塞", "required_for_admission Case 的当前阻塞项", "interface-node-admission");
      admissionPanel.appendChild(renderInterfaceNodeAdmissionBlockers(admission));
      content.appendChild(admissionPanel);
    }
  }
  if (!options.preserveStatus) {
    setInterfaceNodeStatus("ready");
  }
}

function renderInterfaceNodeMissing(error) {
  const payload = error.payload || {};
  interfaceNodeEl("interfaceNodeTitle").textContent = "未找到接口节点";
  interfaceNodeEl("interfaceNodeSummary").textContent = payload.requested || requestedInterfaceNodeId() || "缺少 id";
  setInterfaceNodeStats([
    ["状态", "missing"],
    ["可选节点", (payload.available || []).length],
  ]);
  const content = interfaceNodeEl("interfaceNodeContent");
  content.innerHTML = "";
  const panel = interfaceNodePanel("可选接口节点", "当前 template-config SQLite 中已登记的接口节点", "interface-node-missing-panel");
  const list = document.createElement("div");
  list.className = "environment-node-peer-list";
  (payload.available || []).forEach((item) => {
    const link = document.createElement("a");
    link.className = "environment-node-peer";
    link.href = item.href;
    const strong = document.createElement("strong");
    strong.textContent = item.displayName || item.id;
    const span = document.createElement("span");
    span.textContent = `${item.serviceId || "-"} · ${item.operation || "-"}`;
    link.appendChild(strong);
    link.appendChild(span);
    list.appendChild(link);
  });
  if (!list.children.length) {
    const empty = document.createElement("p");
    empty.className = "dashboard-empty";
    empty.textContent = "还没有登记接口节点配置。";
    list.appendChild(empty);
  }
  panel.appendChild(list);
  content.appendChild(panel);
  setInterfaceNodeStatus("missing");
}

async function loadInterfaceNodeDetail(options = {}) {
  if (!options.silent) {
    setInterfaceNodeStatus("refreshing...");
  }
  const nodeId = requestedInterfaceNodeId();
  if (!nodeId) {
    throw new Error("missing interface node id");
  }
  const payload = await interfaceNodeRequest(`/api/interface-node?id=${encodeURIComponent(nodeId)}`);
  renderInterfaceNodeDetail(payload, options);
}

interfaceNodeEl("refreshInterfaceNodeBtn")?.addEventListener("click", () => {
  loadInterfaceNodeDetail().catch(renderInterfaceNodeMissing);
});

loadInterfaceNodeDetail().catch(renderInterfaceNodeMissing);
