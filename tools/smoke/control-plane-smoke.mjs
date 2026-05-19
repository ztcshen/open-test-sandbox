import { spawn, spawnSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));
const smokeStepIDs = Array.from({ length: 10 }, (_, index) => `step-${String(index + 1).padStart(2, "0")}`);

function smokeTraceOverrides(env = process.env) {
  const raw = String(env.OTS_SMOKE_TRACE_IDS || "").trim();
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return Object.fromEntries(Object.entries(parsed).map(([key, value]) => [key, String(value).trim()]));
    }
  } catch {
    // Accept comma-separated step=trace mappings when JSON is inconvenient in shell.
  }
  return Object.fromEntries(raw.split(",").map((item) => item.split("=").map((part) => part.trim())).filter(([key, value]) => key && value));
}

function envFlag(value) {
  return /^(1|true|yes|on)$/i.test(String(value || "").trim());
}

export function missingSmokeTraceIDSteps(env = process.env) {
  const overrides = smokeTraceOverrides(env);
  return smokeStepIDs.filter((stepID) => !overrides[stepID]);
}

export function requireCompleteSmokeTraceIDs(env = process.env) {
  const missing = missingSmokeTraceIDSteps(env);
  if (missing.length > 0) {
    throw new Error(`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 requires OTS_SMOKE_TRACE_IDS for all 10 workflow steps; missing: ${missing.join(" ")}`);
  }
}

export function smokeTraceID(stepID, defaultTraceID, env = process.env) {
  return smokeTraceOverrides(env)[stepID] || defaultTraceID;
}

const coreSmokeSteps = smokeStepIDs.map((id, index) => {
  const number = String(index + 1).padStart(2, "0");
  return {
    id,
    caseID: `case.step-${number}`,
    nodeID: `node.step-${number}`,
    serviceID: `service.step-${number}`,
    templateID: `template.step-${number}`,
    path: `/v1/items/step-${number}`,
    traceID: smokeTraceID(id, `trace.smoke.${number}`),
  };
});

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: rootDir, encoding: "utf8", stdio: "pipe", ...options });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed\n${result.stdout}\n${result.stderr}`);
  }
}

async function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

async function waitForJSON(url, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { headers: { Accept: "application/json" } });
      if (response.ok) return response.json();
      lastError = new Error(`${url} returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw lastError || new Error(`timed out waiting for ${url}`);
}

async function postJSON(url, body) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(`${url} returned ${response.status}: ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function startSmokeTargetServer(port) {
  const server = createServer((request, response) => {
    if (request.url?.startsWith("/v1/items")) {
      const pathname = new URL(request.url, `http://127.0.0.1:${port}`).pathname;
      const step = coreSmokeSteps.find((item) => item.path === pathname);
      response.writeHead(200, {
        "content-type": "application/json",
        "request-id": step ? `smoke-request-${step.id}` : "smoke-request-1",
      });
      response.end(JSON.stringify({ ok: true, id: step?.id || "item-smoke-1" }));
      return;
    }
    response.writeHead(404, { "content-type": "application/json" });
    response.end(JSON.stringify({ ok: false, error: "not found" }));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

async function startSmokeTraceProvider(port) {
  const server = createServer(async (request, response) => {
    let body = "";
    for await (const chunk of request) {
      body += chunk;
    }
    let payload = {};
    try {
      payload = JSON.parse(body || "{}");
    } catch {
      response.writeHead(400, { "content-type": "application/json" });
      response.end(JSON.stringify({ errors: [{ message: "invalid json" }] }));
      return;
    }
    response.writeHead(200, { "content-type": "application/json" });
    if (payload.query?.includes("queryBasicTraces")) {
      response.end(JSON.stringify({
        data: {
          queryBasicTraces: {
            traces: coreSmokeSteps.map((step) => ({
              endpointNames: [`GET:${step.path}`, step.path],
              duration: 42,
              start: "2026-05-18 1200",
              isError: false,
              traceIds: [step.traceID],
            })),
          },
        },
      }));
      return;
    }
    if (payload.query?.includes("queryTrace")) {
      const traceID = payload.variables?.traceId || "trace.smoke.01";
      const step = coreSmokeSteps.find((item) => item.traceID === traceID) || coreSmokeSteps[0];
      response.end(JSON.stringify({
        data: {
          queryTrace: {
            spans: [
              {
                traceId: step.traceID,
                segmentId: "segment.entry",
                spanId: 0,
                parentSpanId: -1,
                refs: [],
                serviceCode: "service.alpha",
                endpointName: step.path,
                type: "Entry",
                component: "Tomcat",
              },
              {
                traceId: step.traceID,
                segmentId: "segment.worker",
                spanId: 0,
                parentSpanId: -1,
                refs: [{ traceId: step.traceID, parentSegmentId: "segment.entry", parentSpanId: 0, type: "CrossProcess" }],
                serviceCode: "service.worker",
                endpointName: `GET:${step.path}`,
                type: "Entry",
                component: "Server",
              },
            ],
          },
        },
      }));
      return;
    }
    response.end(JSON.stringify({ errors: [{ message: "unexpected query" }] }));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

export async function prepareSmokeTraceProvider(env = process.env) {
  const configuredURL = String(env.OTS_TRACE_GRAPHQL_URL || "").trim();
  if (envFlag(env.OTSANDBOX_REQUIRE_REAL_SKYWALKING)) {
    if (!configuredURL) {
      throw new Error("OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 requires OTS_TRACE_GRAPHQL_URL");
    }
    requireCompleteSmokeTraceIDs(env);
  }
  if (configuredURL) {
    return { graphQLURL: configuredURL, mode: "real", server: null };
  }
  const port = await freePort();
  const server = await startSmokeTraceProvider(port);
  return { graphQLURL: `http://127.0.0.1:${port}`, mode: "synthetic", server };
}

async function closeHTTPServer(server) {
  if (!server) return;
  await new Promise((resolve) => server.close(resolve));
}

export async function writeSmokeProfile(baseDir, targetPort) {
  const profileDir = path.join(baseDir, "profile");
  await mkdir(profileDir, { recursive: true });
  const profile = {
    id: "smoke",
    displayName: "Smoke Profile",
    description: "Generic profile for local browser smoke checks.",
    services: coreSmokeSteps.map((step, index) => ({
      id: step.serviceID,
      displayName: `Service ${index + 1}`,
      kind: "http",
      servicePort: targetPort,
      sortOrder: index + 1,
    })),
    workflows: [{ id: "workflow.alpha", displayName: "Workflow Alpha", description: "Checks a generic item flow." }],
    interfaceNodes: coreSmokeSteps.map((step, index) => ({
      id: step.nodeID,
      displayName: `Node ${index + 1}`,
      serviceId: step.serviceID,
    })),
    apiCases: coreSmokeSteps.map((step, index) => ({
      id: step.caseID,
      displayName: `Case ${index + 1}`,
      nodeId: step.nodeID,
      sortOrder: index + 1,
      requiredForAdmission: true,
    })),
    requestTemplates: coreSmokeSteps.map((step, index) => ({
      id: step.templateID,
      displayName: `Template ${index + 1}`,
      nodeId: step.nodeID,
      method: "GET",
      path: step.path,
      templateJson: JSON.stringify({ method: "GET", path: step.path }),
    })),
    caseDependencies: coreSmokeSteps.map((step) => ({
      id: `dependency.${step.id}`,
      caseId: step.caseID,
      fixtureId: "fixture.alpha",
      mappingsJson: "[]",
    })),
    workflowBindings: coreSmokeSteps.map((step, index) => ({
      workflowId: "workflow.alpha",
      stepId: step.id,
      nodeId: step.nodeID,
      caseId: step.caseID,
      required: true,
      sortOrder: index + 1,
    })),
    fixtures: [{ id: "fixture.alpha", displayName: "Fixture Alpha", kind: "json", dataJson: "{}" }],
    templateConfigs: [
      {
        id: "cfg.workflow-directory.default",
        templateId: "TPL-WORKFLOW-DIRECTORY-V1",
        scopeType: "workflow-directory",
        scopeId: "_default",
        configJson: JSON.stringify({
          workflowFinder: {
            targetStepCount: 10,
            targetInterfaceCount: 10,
            targetLabel: "Configured workflow target",
          },
        }),
        status: "active",
      },
      ...coreSmokeSteps.map((step, index) => ({
        id: `cfg.${step.caseID}.execution`,
        templateId: "case-execution",
        scopeType: "case",
        scopeId: step.caseID,
        configJson: JSON.stringify({
          caseId: step.caseID,
          caseExecution: {
            method: "GET",
            nodeId: step.serviceID,
            path: step.path,
            expectedHttpCodes: [200],
          },
        }),
        status: "active",
        sortOrder: index + 1,
      })),
    ],
  };
  await writeFile(path.join(profileDir, "profile.json"), JSON.stringify(profile, null, 2));
  return profileDir;
}

export async function prepareSmokeStoreReference(tempDir, env = process.env, runCommand = run) {
  const smokeStoreDSN = env.OTSANDBOX_SMOKE_STORE_DSN || env.OTSANDBOX_SMOKE_STORE || "";
  if (!smokeStoreDSN) {
    if (/^(1|true|yes|on)$/i.test(env.OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE || "")) {
      if (/^(1|true|yes|on)$/i.test(env.OTSANDBOX_DISABLE_SQLITE_STORE || "")) {
        throw new Error("OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE cannot be combined with OTSANDBOX_DISABLE_SQLITE_STORE");
      }
      return { storeRef: `sqlite://${path.join(tempDir, "store.sqlite")}`, serverEnv: env };
    }
    throw new Error("set OTSANDBOX_SMOKE_STORE_DSN to a PostgreSQL Store DSN for smoke validation");
  }
  if (!/^postgres(?:ql)?:\/\//i.test(smokeStoreDSN)) {
    throw new Error("OTSANDBOX_SMOKE_STORE_DSN must be a PostgreSQL Store DSN");
  }
  const configHome = path.join(tempDir, "store-config");
  await mkdir(configHome, { recursive: true });
  const serverEnv = { ...env, OTSANDBOX_CONFIG_HOME: configHome };
  runCommand("go", ["run", "./cmd/otsandbox", "store", "config", "set", "smoke-postgres", "--url", smokeStoreDSN], { env: serverEnv });
  runCommand("go", ["run", "./cmd/otsandbox", "store", "use", "smoke-postgres"], { env: serverEnv });
  runCommand("go", ["run", "./cmd/otsandbox", "store", "upgrade", "--store", "smoke-postgres"], { env: serverEnv });
  return { storeRef: "smoke-postgres", serverEnv };
}

function requireValue(value, message) {
  if (value === undefined || value === null || value === "") {
    throw new Error(message);
  }
  return value;
}

export function assertWorkflowCaseEvidence(payload, { runID, caseID, stepID, path = "/v1/items", traceID = "trace.smoke.1" }) {
  if (!payload?.ok || !payload.evidence) {
    throw new Error(`case evidence payload is not ok: ${JSON.stringify(payload)}`);
  }
  const evidence = payload.evidence;
  const summary = evidence.summary || {};
  if (summary.run_id !== runID || summary.case_id !== caseID || summary.step_id !== stepID || summary.status !== "passed") {
    throw new Error(`unexpected case evidence summary: ${JSON.stringify(summary)}`);
  }
  const request = evidence.request || {};
  if (request.method !== "GET" || request.path !== path) {
    throw new Error(`unexpected case evidence request: ${JSON.stringify(request)}`);
  }
  requireValue(request.evidence_uri, `request evidence missing stored URI: ${JSON.stringify(request)}`);
  const response = evidence.response || {};
  if (Number(response.http_code) !== 200) {
    throw new Error(`unexpected case evidence response: ${JSON.stringify(response)}`);
  }
  requireValue(response.evidence_uri, `response evidence missing stored URI: ${JSON.stringify(response)}`);
  const assertions = evidence.assertions || {};
  if (assertions.status !== "passed" || assertions.passed !== true) {
    throw new Error(`unexpected case evidence assertions: ${JSON.stringify(assertions)}`);
  }
  const topology = evidence.topology || {};
  assertSkyWalkingTopologyEvidence(topology, { traceID });
}

export function assertSkyWalkingTopologyEvidence(topology, { traceID, source = "service.alpha", target = "service.worker" } = {}) {
  const edges = Array.isArray(topology?.confirmedEdges) ? topology.confirmedEdges : [];
  const nodes = Array.isArray(topology?.observedNodes) ? topology.observedNodes : [];
  const edge = edges.find((item) => item?.source === source && item?.target === target);
  if (
    topology?.provider !== "skywalking"
    || topology?.status !== "complete"
    || (traceID && topology?.traceId !== traceID)
    || !nodes.includes(source)
    || !nodes.includes(target)
    || !edge
  ) {
    throw new Error(`case evidence missing complete SkyWalking topology evidence: ${JSON.stringify(topology)}`);
  }
}

async function checkPage(browser, baseURL, pageSpec) {
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(baseURL + pageSpec.path, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`${pageSpec.path} returned ${response?.status()}`);
    }
    await page.waitForSelector(pageSpec.root);
    const text = (await page.locator(pageSpec.root).innerText()).trim();
    if (text.length < 20) {
      throw new Error(`${pageSpec.path} rendered too little text: ${JSON.stringify(text)}`);
    }
    for (const presentText of pageSpec.presentText || []) {
      if (!text.includes(presentText)) {
        throw new Error(`${pageSpec.path} missing expected text: ${presentText}`);
      }
    }
    for (const absentText of pageSpec.absentText || []) {
      if (text.includes(absentText)) {
        throw new Error(`${pageSpec.path} still renders removed text: ${absentText}`);
      }
    }
    for (const absentHref of pageSpec.absentHrefs || []) {
      const count = await page.locator(`a[href*="${absentHref}"]`).count();
      if (count > 0) {
        throw new Error(`${pageSpec.path} still links to removed href: ${absentHref}`);
      }
    }
    for (const presentHref of pageSpec.presentHrefs || []) {
      const count = await page.locator(`a[href="${presentHref}"]`).count();
      if (count === 0) {
        throw new Error(`${pageSpec.path} missing expected href: ${presentHref}`);
      }
    }
    if (errors.length > 0) {
      throw new Error(`${pageSpec.path} browser errors:\n${errors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function checkWorkflowDetailRunButton(browser, baseURL) {
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(`${baseURL}/workflow-detail.html?id=workflow.alpha`, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`/workflow-detail.html returned ${response?.status()}`);
    }
    await page.waitForSelector("#react-workflow-detail-root");
    await page.getByRole("button", { name: "运行 Workflow" }).click();
    try {
      await page.waitForFunction((expected) => document.querySelectorAll(".workflow-progress-step.passed").length === expected, coreSmokeSteps.length, { timeout: 30000 });
      await page.locator(".workflow-run-template .status-pill.passed", { hasText: "passed" }).waitFor({ timeout: 30000 });
    } catch (error) {
      const text = await page.locator(".workflow-run-template").innerText().catch(() => "");
      throw new Error(`/workflow-detail.html did not complete after clicking run button:\n${text}\n${error.message}`);
    }
    const passedSteps = await page.locator(".workflow-progress-step.passed").count();
    if (passedSteps !== coreSmokeSteps.length) {
      throw new Error(`/workflow-detail.html expected ${coreSmokeSteps.length} passed workflow steps, got ${passedSteps}`);
    }
    const persistedRunLink = page.locator('a[href^="/workflow-run.html?id="]').first();
    try {
      await persistedRunLink.waitFor({ timeout: 10000 });
    } catch (error) {
      const text = await page.locator(".workflow-run-template").innerText().catch(() => "");
      throw new Error(`/workflow-detail.html did not expose the persisted workflow run link:\n${text}\n${error.message}`);
    }
    const href = await persistedRunLink.getAttribute("href");
    if (errors.length > 0) {
      throw new Error(`/workflow-detail.html run button browser errors:\n${errors.join("\n")}`);
    }
    const runID = new URL(href, baseURL).searchParams.get("id");
    const detail = runID ? await waitForJSON(`${baseURL}/api/workflow-runs/${encodeURIComponent(runID)}`) : {};
    const topologies = Array.isArray(detail.traceTopologies) ? detail.traceTopologies : [];
    for (const step of coreSmokeSteps) {
      const topology = topologies.find((item) => item.stepId === step.id && item.provider === "skywalking");
      if (!topology) {
        throw new Error(`/workflow-detail.html run did not persist SkyWalking topology for ${step.id}: ${JSON.stringify({ runID, topologies, summary: detail.summary })}`);
      }
      const parsed = typeof topology.topologyJson === "string" ? JSON.parse(topology.topologyJson) : topology.topologyJson;
      assertSkyWalkingTopologyEvidence(parsed, { traceID: step.traceID });
    }
    return runID;
  } finally {
    await page.close();
  }
}

async function checkWorkflowStepSkyWalkingTopology(browser, baseURL, runID) {
  if (!runID) {
    throw new Error("workflow run button did not return a run id for topology verification");
  }
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const step = coreSmokeSteps[0];
    const stepURL = `${baseURL}/workflow-step.html?workflow=workflow.alpha&step=${step.id}&runId=${encodeURIComponent(runID)}`;
    const response = await page.goto(stepURL, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`/workflow-step.html returned ${response?.status()}`);
    }
    await page.waitForSelector("#react-workflow-step-root");
    await page.locator(".workflow-step-topology-head", { hasText: "complete" }).waitFor({ timeout: 30000 });
    const text = await page.locator(".workflow-step-topology-graph").innerText();
    for (const expected of ["2 nodes", "1 edges", "complete", step.traceID, "service.alpha", "service.worker"]) {
      if (!text.includes(expected)) {
        throw new Error(`/workflow-step.html SkyWalking topology missing ${expected}:\n${text}`);
      }
    }
    const detail = await waitForJSON(`${baseURL}/api/workflow-runs/${encodeURIComponent(runID)}`);
    const topologies = detail.traceTopologies || [];
    const topology = topologies.find((item) => item.stepId === step.id && item.provider === "skywalking");
    if (!topology) {
      throw new Error(`/api/workflow-runs/${runID} missing stored SkyWalking topology: ${JSON.stringify(topologies)}`);
    }
    const parsed = typeof topology.topologyJson === "string" ? JSON.parse(topology.topologyJson) : topology.topologyJson;
    assertSkyWalkingTopologyEvidence(parsed, { traceID: step.traceID });
    const tasks = await waitForJSON(`${baseURL}/api/post-process-tasks?runId=${encodeURIComponent(runID)}&stepId=${step.id}&kind=trace_topology_collect`);
    if (tasks.counts?.passed !== 1 || tasks.counts?.failed !== 0 || tasks.counts?.skipped !== 0 || tasks.tasks?.[0]?.status !== "passed") {
      throw new Error(`unexpected SkyWalking post-process task status: ${JSON.stringify(tasks)}`);
    }
    if (errors.length > 0) {
      throw new Error(`/workflow-step.html topology browser errors:\n${errors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function checkWorkflowRunCaseEvidence(baseURL, runID) {
  if (!runID) {
    throw new Error("workflow run button did not return a run id for evidence verification");
  }
  for (const step of coreSmokeSteps) {
    const payload = await waitForJSON(`${baseURL}/api/case/evidence?runId=${encodeURIComponent(runID)}&caseId=${encodeURIComponent(step.caseID)}&stepId=${encodeURIComponent(step.id)}`);
    assertWorkflowCaseEvidence(payload, { runID, caseID: step.caseID, stepID: step.id, path: step.path, traceID: step.traceID });
  }
}

async function checkEvidenceViewerTimeline(browser, baseURL) {
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const storageKey = "open-test-sandbox-evidence:smoke-timeline";
    const payload = {
      step: {
        title: "Case Alpha Evidence",
        goal: "POST /items",
        stageTitle: "API Case",
        caseId: "case.alpha",
        path: "service.alpha",
        correlators: ["req-1"],
        systems: [
          {
            id: "service.alpha",
            name: "Service Alpha",
            found: true,
            coreLogs: ["2026-05-18T01:00:00Z request_id=req-1 create item", "2026-05-18T01:00:01Z response 500"],
          },
        ],
        topology: { provider: "skywalking", status: "complete", requestId: "req-1", traceId: "trace-1", confirmedEdges: [{ source: "service.alpha", target: "service.worker" }] },
      },
      caseDiagnostics: {
        summary: { case_id: "case.alpha", operation: "POST /items", evidence_path: ".runtime/evidence/smoke-timeline" },
        request: { method: "POST", path: "/items", request_id: "req-1" },
        response: { http_code: 500, request_id: "req-1" },
        assertions: { status: "failed", passed: false, http_status_ok: false, failure_reason: "unexpected status" },
        fixture: { status: "configured", applyRuns: [{ status: "applied", fixtureInstanceId: "fixture-1" }], dependencies: [{ id: "dependency.alpha" }], summary: { applyCount: 1, dependencyCount: 1 } },
        topology: { provider: "skywalking", status: "complete", requestId: "req-1", traceId: "trace-1", confirmedEdges: [{ source: "service.alpha", target: "service.worker" }] },
        artifacts: [{ label: "case bundle", path: "/api/case/evidence?caseRun=run.alpha&caseId=case.alpha", kind: "json" }],
      },
    };
    await page.goto(`${baseURL}/index.html`, { waitUntil: "networkidle" });
    await page.evaluate(({ key, value }) => localStorage.setItem(key, JSON.stringify(value)), { key: storageKey, value: payload });
    const response = await page.goto(`${baseURL}/evidence-viewer.html?key=${encodeURIComponent(storageKey)}&workflow=workflow.alpha&caseId=case.alpha`, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`/evidence-viewer.html returned ${response?.status()}`);
    }
    await page.waitForSelector("#react-evidence-viewer-root");
    await page.getByText("Workflow case set").waitFor();
    const workflowContextLink = await page.locator('a[href="/api-cases.html?workflow=workflow.alpha&case=case.alpha"]').count();
    if (workflowContextLink === 0) {
      throw new Error("/evidence-viewer.html missing workflow case set handoff");
    }
    const caseRunsContextLink = await page.locator('a[href="/case-runs.html?case=case.alpha&workflow=workflow.alpha"]').count();
    if (caseRunsContextLink === 0) {
      throw new Error("/evidence-viewer.html missing workflow-scoped case run handoff");
    }
    await page.getByText("Evidence Timeline").waitFor();
    await page.getByText("Evidence Artifacts").waitFor();
    await page.getByText("Reproduction Command").waitFor();
    await page.locator(".viewer-reproduction-card pre", { hasText: "curl -i -X POST" }).waitFor();
    await page.locator(".viewer-artifact-item strong", { hasText: "case bundle" }).waitFor();
    await page.locator(".viewer-artifact-item code", { hasText: ".runtime/evidence/smoke-timeline" }).waitFor();
    await page.getByText("request 1").waitFor();
    await page.getByText("response 1").waitFor();
    await page.getByText("assertions 1").waitFor();
    await page.locator("button.detail-tab", { hasText: "logs 1" }).click();
    await page.locator(".viewer-timeline-detail h3", { hasText: "Service Alpha" }).waitFor();
    await page.getByPlaceholder("request / log / status").fill("response 500");
    await page.locator(".viewer-timeline-detail pre", { hasText: "response 500" }).waitFor();
    if (errors.length > 0) {
      throw new Error(`/evidence-viewer.html timeline browser errors:\n${errors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function checkWorkbenchVerify(browser, baseURL, profileDir) {
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(`${baseURL}/index.html`, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`/index.html returned ${response?.status()}`);
    }
    await page.locator("input[type='text']").first().fill(profileDir);
    await page.getByRole("button", { name: "验收并发布" }).click();
    await page.getByText("all passed").waitFor();
    await page.getByText("profile-index").waitFor();
    await page.getByText("case runs optional").waitFor();
    await page.getByText("workflow runs optional").waitFor();
    await page.getByLabel("要求用例已通过").check();
    await page.getByRole("button", { name: "验收并发布" }).click();
    await page.getByText("case runs required").waitFor();
    const missingCaseRuns = await page.getByText("10 failed").waitFor({ timeout: 5000 }).then(() => true).catch(() => false);
    if (missingCaseRuns) {
      await page.getByText("api-case-run:case.step-01", { exact: true }).waitFor();
    } else {
      await page.getByText("all passed").waitFor();
    }
    const unexpectedErrors = errors.filter((item) => !item.includes("400 (Bad Request)"));
    if (unexpectedErrors.length > 0) {
      throw new Error(`/index.html verify action browser errors:\n${unexpectedErrors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function checkWorkbenchInvalidInstalledProfile(browser, baseURL, profileHome) {
  const brokenDir = path.join(profileHome, "broken");
  await mkdir(brokenDir, { recursive: true });
  await writeFile(path.join(brokenDir, "profile.json"), `{"id":`);

  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(`${baseURL}/index.html`, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`/index.html returned ${response?.status()}`);
    }
    await page.locator("select option").filter({ hasText: "broken · invalid" }).waitFor({ state: "attached" });
    const invalidOption = await page.locator("select").evaluate((select) => {
      const option = Array.from(select.options).find((item) => item.textContent.includes("broken · invalid"));
      return option ? { disabled: option.disabled, text: option.textContent } : null;
    });
    if (!invalidOption?.disabled) {
      throw new Error(`invalid installed profile option should be disabled: ${JSON.stringify(invalidOption)}`);
    }
    if (errors.length > 0) {
      throw new Error(`/index.html invalid profile browser errors:\n${errors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function stopServer(server) {
  if (server.exitCode !== null || server.signalCode !== null) return;
  const closed = new Promise((resolve) => server.once("close", resolve));
  try {
    process.kill(-server.pid, "SIGTERM");
  } catch {
    server.kill("SIGTERM");
  }
  const stopped = await Promise.race([
    closed.then(() => true),
    new Promise((resolve) => setTimeout(() => resolve(false), 3000)),
  ]);
  if (!stopped) {
    try {
      process.kill(-server.pid, "SIGKILL");
    } catch {
      server.kill("SIGKILL");
    }
    await Promise.race([
      closed,
      new Promise((resolve) => setTimeout(resolve, 3000)),
    ]);
  }
}

async function main() {
  run("node", ["control-plane/frontend/build.mjs"]);

  const tempDir = await mkdtemp(path.join(os.tmpdir(), "otsandbox-smoke-"));
  const targetPort = await freePort();
  const targetServer = await startSmokeTargetServer(targetPort);
  const traceProvider = await prepareSmokeTraceProvider();
  const profileDir = await writeSmokeProfile(tempDir, targetPort);
  const profileHome = path.join(tempDir, "profile-home");
  const { storeRef, serverEnv } = await prepareSmokeStoreReference(tempDir);
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const server = spawn("go", [
    "run",
    "./cmd/otsandbox",
    "serve",
    "--profile",
    profileDir,
    "--profile-home",
    profileHome,
    "--store",
    storeRef,
    "--host",
    "127.0.0.1",
    "--port",
    String(port),
    "--trace-graphql-url",
    traceProvider.graphQLURL,
  ], {
    cwd: rootDir,
    env: serverEnv,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });

  let output = "";
  server.stdout.on("data", (chunk) => {
    output += chunk;
  });
  server.stderr.on("data", (chunk) => {
    output += chunk;
  });

  try {
    const profile = await waitForJSON(`${baseURL}/api/template-packages/current`);
    if (profile.templatePackageId !== "smoke") throw new Error(`unexpected template package payload: ${JSON.stringify(profile)}`);

    const imported = await postJSON(`${baseURL}/api/template-packages/import`, { templatePackagePath: profileDir });
    if (imported.profileId !== "smoke") throw new Error(`unexpected import payload: ${JSON.stringify(imported)}`);

    const index = await waitForJSON(`${baseURL}/api/template-packages/catalog-index`);
    if (index.profileId !== "smoke" || index.counts.workflows !== 1 || index.counts.templates !== 22 || index.counts.templateConfigs !== 22) {
      throw new Error(`unexpected catalog index: ${JSON.stringify(index)}`);
    }
    const catalog = await waitForJSON(`${baseURL}/api/catalog`);
    const finder = catalog.presentation?.workflowFinder;
    if (finder?.targetStepCount !== 10 || finder?.targetInterfaceCount !== 10 || finder?.targetLabel !== "Configured workflow target") {
      throw new Error(`unexpected workflow finder config: ${JSON.stringify(catalog.presentation)}`);
    }

    const browser = await chromium.launch({ headless: true });
    try {
      const pages = [
        { path: "/index.html", root: "#react-sandbox-workbench-root", presentText: ["Configured workflow target", "MATCHING WORKFLOW", "Workflow Alpha", "安装到本地", "要求用例已通过", "要求工作流已通过", "验收并发布"], absentText: ["Agent Test Kit"], absentHrefs: ["agent-test.html"] },
        { path: "/dashboard.html", root: "#react-dashboard-root" },
        { path: "/workflows.html", root: "#react-workflows-root", presentText: ["Configured workflow target", "WORKFLOW MAP", "STEP", "INTERFACE", "CASE", "ACTIONS", "Runs", "ready"], presentHrefs: ["/api-cases.html?workflow=workflow.alpha&case=case.step-01"] },
        { path: "/workflow-detail.html?id=workflow.alpha", root: "#react-workflow-detail-root" },
        { path: "/workflow-blueprint-demo.html?workflow=workflow.alpha", root: "#react-workflow-blueprint-demo-root" },
        { path: "/workflow-blueprint-new.html", root: "#react-workflow-blueprint-demo-root" },
        { path: "/api-cases.html", root: "#react-api-cases-root", presentText: ["API Case 工作台", "Coverage matrix", "Case Management Search", "Readiness groups"] },
        { path: "/api-cases.html?workflow=workflow.alpha", root: "#react-api-cases-root", presentText: ["WORKFLOW CASE SET", "Workflow Alpha", "10 steps", "10 interfaces", "10 cases", "Workflow case sequence", "Case 1", "service.step-01", "Runs"], presentHrefs: ["/interface-nodes.html?serviceId=service.step-01&workflow=workflow.alpha&case=case.step-01", "/case-runs.html?case=case.step-01&workflow=workflow.alpha"] },
        { path: "/interface-nodes.html?serviceId=service.step-01&workflow=workflow.alpha&case=case.step-01", root: "#react-interface-nodes-root", presentText: ["Workflow case set", "Node 1", "service.step-01"], presentHrefs: ["/interface-node.html?id=node.step-01&workflow=workflow.alpha&case=case.step-01", "/api-cases.html?workflow=workflow.alpha&case=case.step-01"] },
        { path: "/interface-node.html?id=node.step-01&workflow=workflow.alpha&case=case.step-01", root: "#react-interface-node-root", presentText: ["Workflow case set"], presentHrefs: ["/api-cases.html?workflow=workflow.alpha&case=case.step-01"] },
        { path: "/case-runs.html", root: "#react-case-runs-root", presentText: ["Run Analysis Center", "Case run report workbench", "Failure triage", "Report Grid"] },
        { path: "/case-runs.html?case=case.step-01", root: "#react-case-runs-root", presentText: ["Run Analysis Center", "case: case.step-01", "CASE EXECUTION SUMMARY", "Report Grid"] },
        { path: "/case-runs.html?workflow=workflow.alpha&case=case.step-01", root: "#react-case-runs-root", presentText: ["Run Analysis Center", "WORKFLOW CONTEXT", "workflow.alpha", "Workflow case set", "case: case.step-01", "CASE EXECUTION SUMMARY"], presentHrefs: ["/api-cases.html?workflow=workflow.alpha&case=case.step-01"] },
        { path: "/interface-nodes.html", root: "#react-interface-nodes-root" },
      ];
      for (const page of pages) {
        await checkPage(browser, baseURL, page);
      }
      await checkEvidenceViewerTimeline(browser, baseURL);
      await checkWorkbenchVerify(browser, baseURL, profileDir);
      await checkWorkbenchInvalidInstalledProfile(browser, baseURL, profileHome);
      const runID = await checkWorkflowDetailRunButton(browser, baseURL);
      await checkWorkflowRunCaseEvidence(baseURL, runID);
      await checkWorkflowStepSkyWalkingTopology(browser, baseURL, runID);
      const removedPage = await fetch(`${baseURL}/agent-test.html`);
      if (removedPage.status !== 404) {
        throw new Error(`/agent-test.html returned ${removedPage.status}, want 404`);
      }
    } finally {
      await browser.close();
    }
    console.log(`control-plane smoke passed on ${baseURL} with ${traceProvider.mode} SkyWalking GraphQL provider`);
  } finally {
    await closeHTTPServer(traceProvider.server);
    await closeHTTPServer(targetServer);
    await stopServer(server);
    await rm(tempDir, { recursive: true, force: true });
    if (server.exitCode !== 0 && server.exitCode !== null && !output.includes("Server closed")) {
      process.stderr.write(output);
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error);
    process.exit(1);
  });
}
