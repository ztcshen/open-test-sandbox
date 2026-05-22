import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { prepareSmokeTraceProvider, smokeWorkflowStepCount, writeSmokeProfile } from "./control-plane-smoke.mjs";
import { requireSafeMySQLStoreDSN } from "./mysql-store-dsn-guard.mjs";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));
const workflowStepCount = smokeWorkflowStepCount;

export function requiredMySQLDSN(env = process.env) {
  const dsn = env.AGENT_TESTBENCH_MYSQL_API_SMOKE_DSN || env.AGENT_TESTBENCH_SMOKE_STORE_DSN || env.AGENT_TESTBENCH_SMOKE_STORE || "";
  if (!dsn.trim()) {
    throw new Error("Set AGENT_TESTBENCH_MYSQL_API_SMOKE_DSN, AGENT_TESTBENCH_SMOKE_STORE_DSN, or AGENT_TESTBENCH_SMOKE_STORE to run the MySQL Store API smoke");
  }
  requireSafeMySQLStoreDSN(dsn, { label: "The MySQL Store API smoke" });
  return dsn;
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

async function startTargetServer(port) {
  const server = createServer((request, response) => {
    const pathname = new URL(request.url || "/", `http://127.0.0.1:${port}`).pathname;
    if (pathname === "/health") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ ok: true, status: "healthy" }));
      return;
    }
    const match = pathname.match(/^\/v1\/items\/(step-\d{2})$/);
    if (!match) {
      response.writeHead(404, { "content-type": "application/json" });
      response.end(JSON.stringify({ ok: false, error: "not found" }));
      return;
    }
    response.writeHead(200, {
      "content-type": "application/json",
      "request-id": `mysql-api-smoke-request-${match[1]}`,
    });
    response.end(JSON.stringify({ ok: true, stepId: match[1] }));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: rootDir,
      env: { ...process.env, ...options.env },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => { stdout += chunk; });
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed with ${code}\n${stdout}\n${stderr}`));
    });
  });
}

async function runJSON(command, args, options = {}) {
  const result = await runCommand(command, args, options);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`${command} ${args.join(" ")} did not emit JSON\n${result.stdout}\n${result.stderr}\n${error.message}`);
  }
}

function buildCLI(outputPath) {
  return runCommand("go", ["build", "-o", outputPath, "./cmd/agent-testbench"]);
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

export function assertWorkflowBatchReport(report, { expectedSteps = workflowStepCount, workflowID = "workflow.alpha" } = {}) {
  if (!report?.ok || report?.status !== "passed") {
    throw new Error(`workflow batch report did not pass: ${JSON.stringify(report)}`);
  }
  if (report.workflowId !== workflowID) {
    throw new Error(`workflow batch report used ${report.workflowId}, expected ${workflowID}: ${JSON.stringify(report)}`);
  }
  if (report.total !== expectedSteps || report.completed !== expectedSteps || report.passed !== expectedSteps || report.failed !== 0) {
    throw new Error(`workflow batch report counts are not ${expectedSteps}/0: ${JSON.stringify(report)}`);
  }
  const cases = Array.isArray(report.cases) ? report.cases : [];
  if (cases.length !== expectedSteps) {
    throw new Error(`workflow batch report has ${cases.length} case rows, expected ${expectedSteps}: ${JSON.stringify(report)}`);
  }
  cases.forEach((item, index) => {
    const stepID = `step-${String(index + 1).padStart(2, "0")}`;
    const caseID = `case.${stepID}`;
    if (item.status !== "passed" || item.stepId !== stepID || item.caseId !== caseID || !item.runId || !item.caseRunId) {
      throw new Error(`workflow batch report row ${index + 1} is incomplete: ${JSON.stringify(item)}`);
    }
  });
}

export function assertCaseEvidencePayload(payload, { runID, caseID, stepID, path: expectedPath }) {
  if (!payload?.ok || !payload.evidence) {
    throw new Error(`case evidence payload is not ok: ${JSON.stringify(payload)}`);
  }
  const evidence = payload.evidence;
  const summary = evidence.summary || {};
  if (summary.run_id !== runID || summary.case_id !== caseID || summary.step_id !== stepID || summary.status !== "passed") {
    throw new Error(`unexpected case evidence summary: ${JSON.stringify(summary)}`);
  }
  const request = evidence.request || {};
  if (request.method !== "GET" || request.path !== expectedPath || !request.evidence_uri) {
    throw new Error(`unexpected case evidence request: ${JSON.stringify(request)}`);
  }
  const response = evidence.response || {};
  if (Number(response.http_code) !== 200 || !response.evidence_uri) {
    throw new Error(`unexpected case evidence response: ${JSON.stringify(response)}`);
  }
  const assertions = evidence.assertions || {};
  if (assertions.status !== "passed" || assertions.passed !== true) {
    throw new Error(`unexpected case evidence assertions: ${JSON.stringify(assertions)}`);
  }
}

export function assertRegisteredInterfaceCatalog({ catalog, detail }, {
  interfaceID = "interface.mysql-api-smoke",
  serviceID = "service.mysql-api-smoke",
  caseID = "case.mysql-api-smoke.default",
  templateID = "template.mysql-api-smoke",
} = {}) {
  const node = detail?.node;
  if (!node || node.serviceId !== serviceID) {
    throw new Error(`MySQL API interface registration did not persist interface node: ${JSON.stringify(node)}`);
  }
  const apiCase = detail?.cases?.find((item) => item.id === caseID);
  if (!apiCase || apiCase.requiredForAdmission !== true) {
    throw new Error(`MySQL API interface registration did not persist API case: ${JSON.stringify(detail?.cases)}`);
  }
  const template = detail?.requestTemplates?.find((item) => item.id === templateID);
  if (!template || template.method !== "GET" || template.path !== "/v1/mysql-api-smoke") {
    throw new Error(`MySQL API interface registration did not persist request template: ${JSON.stringify(detail?.requestTemplates)}`);
  }
  const catalogCase = catalog?.apiCases?.find((item) => item.id === caseID);
  if (!catalogCase || catalogCase.nodeId !== interfaceID) {
    throw new Error(`MySQL API interface registration did not expose the case in catalog: ${JSON.stringify(catalog?.apiCases)}`);
  }
}

export function assertEnvironmentCatalogPayload({ registered, discoverAll, inspect, bootstrap }, {
  environmentID = "env.mysql-api-smoke",
  workflowID = "workflow.alpha",
} = {}) {
  const registeredEnv = registered?.environment || {};
  if (!registered?.ok || registeredEnv.id !== environmentID || registeredEnv.status !== "draft" || registeredEnv.verified !== false || registeredEnv.verificationWorkflowId !== workflowID) {
    throw new Error(`unexpected MySQL Environment Catalog registration payload: ${JSON.stringify(registered)}`);
  }
  const discoverItems = Array.isArray(discoverAll?.items) ? discoverAll.items : [];
  const discovered = discoverItems.find((item) => item.id === environmentID);
  if (!discoverAll?.ok || !discovered || discovered.verificationWorkflowId !== workflowID) {
    throw new Error(`MySQL Environment Catalog discovery did not include ${environmentID}: ${JSON.stringify(discoverAll)}`);
  }
  const inspectedEnv = inspect?.environment || {};
  if (!inspect?.ok || inspectedEnv.id !== environmentID || inspectedEnv.verificationWorkflowId !== workflowID) {
    throw new Error(`unexpected MySQL Environment Catalog inspect payload: ${JSON.stringify(inspect)}`);
  }
  const plan = bootstrap?.plan || {};
  const docker = plan.restore?.docker || {};
  if (!bootstrap?.ok || plan.verificationWorkflow !== workflowID || docker.action !== "docker-compose") {
    throw new Error(`unexpected MySQL Environment Catalog bootstrap payload: ${JSON.stringify(bootstrap)}`);
  }
}

export function assertEnvironmentAcceptancePayload({ report, inspect }, {
  environmentID = "env.mysql-api-smoke",
  workflowID = "workflow.alpha",
  expectedSteps = workflowStepCount,
} = {}) {
  if (!report?.ok || report.environmentId !== environmentID || report.workflowId !== workflowID || report.status !== "passed") {
    throw new Error(`unexpected MySQL Environment acceptance report: ${JSON.stringify(report)}`);
  }
  if (report.total !== expectedSteps || report.completed !== expectedSteps || report.passed !== expectedSteps || report.failed !== 0) {
    throw new Error(`MySQL Environment acceptance counts are not ${expectedSteps}/0: ${JSON.stringify(report)}`);
  }
  const acceptance = report.acceptance || {};
  if (!acceptance.ok || acceptance.workflowId !== workflowID || acceptance.topologyProvider !== "skywalking") {
    throw new Error(`MySQL Environment acceptance did not pass the SkyWalking template: ${JSON.stringify(acceptance)}`);
  }
  if (acceptance.expectedSteps !== expectedSteps || acceptance.completedSteps !== expectedSteps || acceptance.passedSteps !== expectedSteps || acceptance.failedSteps !== 0) {
    throw new Error(`MySQL Environment acceptance step counts are not ${expectedSteps}/0: ${JSON.stringify(acceptance)}`);
  }
  if (acceptance.healthSummary?.failed !== 0 || Number(acceptance.healthSummary?.passed || 0) < 1) {
    throw new Error(`MySQL Environment acceptance health checks did not pass: ${JSON.stringify(acceptance.healthSummary)}`);
  }
  const steps = Array.isArray(acceptance.steps) ? acceptance.steps : [];
  if (steps.length !== expectedSteps || steps.some((step) => step.evidenceComplete !== true || step.topologyComplete !== true)) {
    throw new Error(`MySQL Environment acceptance did not prove Evidence and topology for all steps: ${JSON.stringify(steps)}`);
  }
  const env = inspect?.environment || {};
  if (!inspect?.ok || env.id !== environmentID || env.status !== "verified-ready" || env.lastVerificationRunId !== report.batchRunId || env.lastVerificationStatus !== "passed" || env.evidenceComplete !== true || env.topologyComplete !== true) {
    throw new Error(`MySQL Environment acceptance status was not persisted: ${JSON.stringify(inspect)}`);
  }
}

export function assertEnvironmentPublishedPayload({ published, discoverVerified, inspect }, {
  environmentID = "env.mysql-api-smoke",
} = {}) {
  const publishedEnv = published?.environment || {};
  if (!published?.ok || publishedEnv.id !== environmentID || publishedEnv.status !== "verified" || publishedEnv.verified !== true) {
    throw new Error(`unexpected MySQL Environment publish-verified payload: ${JSON.stringify(published)}`);
  }
  const discovered = (Array.isArray(discoverVerified?.items) ? discoverVerified.items : []).find((item) => item.id === environmentID);
  if (!discoverVerified?.ok || !discovered || discovered.status !== "verified" || discovered.verified !== true) {
    throw new Error(`published MySQL Environment was not returned by default verified discovery: ${JSON.stringify(discoverVerified)}`);
  }
  const env = inspect?.environment || {};
  if (!inspect?.ok || env.id !== environmentID || env.status !== "verified" || env.verified !== true || env.evidenceComplete !== true || env.topologyComplete !== true) {
    throw new Error(`published MySQL Environment status was not persisted: ${JSON.stringify(inspect)}`);
  }
}

async function waitForWorkflowBatchReport(baseURL, reportURL, timeoutMs = 30000) {
  if (!reportURL) {
    throw new Error("workflow batch start did not return reportUrl");
  }
  const deadline = Date.now() + timeoutMs;
  let lastReport;
  let lastError;
  while (Date.now() < deadline) {
    try {
      lastReport = await waitForJSON(`${baseURL}${reportURL}`, 5000);
      if (lastReport.status !== "running") {
        assertWorkflowBatchReport(lastReport);
        return lastReport;
      }
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw lastError || new Error(`timed out waiting for workflow batch report: ${JSON.stringify(lastReport)}`);
}

async function waitForEnvironmentAcceptanceReport(baseURL, reportURL, timeoutMs = 30000) {
  if (!reportURL) {
    throw new Error("environment acceptance start did not return reportUrl");
  }
  const deadline = Date.now() + timeoutMs;
  let lastReport;
  let lastError;
  while (Date.now() < deadline) {
    try {
      lastReport = await waitForJSON(`${baseURL}${reportURL}`, 5000);
      if (lastReport.status !== "running") {
        return lastReport;
      }
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw lastError || new Error(`timed out waiting for environment acceptance report: ${JSON.stringify(lastReport)}`);
}

async function stopServer(server) {
  if (!server) return;
  if (server.exitCode !== null || server.signalCode) return;
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

async function closeHTTPServer(server) {
  if (!server) return;
  await new Promise((resolve) => server.close(resolve));
}

function assertNoRawSecret(payload, rawDSN) {
  let rawPassword = "";
  try {
    rawPassword = new URL(rawDSN).password;
  } catch {
    const passwordMatch = rawDSN.match(/^mysql:\/\/[^:/?#]+:([^@/?#]+)@/i);
    rawPassword = passwordMatch?.[1] || "";
  }
  if (!rawPassword) {
    return;
  }
  const raw = JSON.stringify(payload);
  const encodedPassword = encodeURIComponent(rawPassword);
  if (raw.includes(`:${rawPassword}@`) || raw.includes(`:${encodedPassword}@`)) {
    throw new Error(`/api/store/current leaked the raw Store password: ${raw}`);
  }
  if (typeof payload?.url === "string") {
    try {
      const visibleURL = new URL(payload.url);
      if (visibleURL.password && visibleURL.password !== "xxxxx") {
        throw new Error(`/api/store/current returned an unmasked Store password: ${raw}`);
      }
    } catch (error) {
      if (error instanceof TypeError) {
        return;
      }
      throw error;
    }
  }
}

async function main() {
  const dsn = requiredMySQLDSN();
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-mysql-api-smoke-"));
  const storeName = "api-mysql";
  let server;
  let targetServer;
  let traceProvider;
  try {
    const cliBin = path.join(tempDir, "agent-testbench");
    await buildCLI(cliBin);
    const env = {
      AGENT_TESTBENCH_CONFIG_HOME: path.join(tempDir, "config"),
      AGENT_TESTBENCH_DISABLE_SQLITE_STORE: "1",
    };
    await mkdir(env.AGENT_TESTBENCH_CONFIG_HOME, { recursive: true });

    await runCommand(cliBin, ["store", "config", "set", storeName, "--url", dsn], { env });
    await runCommand(cliBin, ["store", "use", storeName], { env });
    await runCommand(cliBin, ["store", "upgrade", "--store", storeName], { env });
    const currentCLI = await runJSON(cliBin, ["store", "current", "--json"], { env });
    if (currentCLI?.name !== storeName || currentCLI?.backend !== "mysql") {
      throw new Error(`CLI active Store is not the named MySQL Store: ${JSON.stringify(currentCLI)}`);
    }

    const targetPort = await freePort();
    targetServer = await startTargetServer(targetPort);
    traceProvider = await prepareSmokeTraceProvider();
    const profileDir = await writeSmokeProfile(tempDir, targetPort);
    const profileHome = path.join(tempDir, "profile-home");
    const port = await freePort();
    const baseURL = `http://127.0.0.1:${port}`;
    server = spawn(cliBin, [
      "serve",
      "--profile",
      profileDir,
      "--profile-home",
      profileHome,
      "--store",
      storeName,
      "--host",
      "127.0.0.1",
      "--port",
      String(port),
    ], {
      cwd: rootDir,
      env: { ...process.env, ...env, AGENT_TESTBENCH_TRACE_GRAPHQL_URL: traceProvider.graphQLURL },
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let output = "";
    server.stdout.on("data", (chunk) => { output += chunk; });
    server.stderr.on("data", (chunk) => { output += chunk; });

    const currentHTTP = await waitForJSON(`${baseURL}/api/store/current`);
    if (!currentHTTP?.ok || !currentHTTP.configured || currentHTTP.name !== storeName || currentHTTP.backend !== "mysql" || currentHTTP.source !== "store-config") {
      throw new Error(`unexpected /api/store/current payload: ${JSON.stringify(currentHTTP)}\n${output}`);
    }
    assertNoRawSecret(currentHTTP, dsn);

    const index = await waitForJSON(`${baseURL}/api/template-packages/catalog-index`);
    if (index.profileId !== "smoke" || index.counts?.workflows !== 1 || index.counts?.apiCases !== workflowStepCount) {
      throw new Error(`unexpected MySQL catalog index: ${JSON.stringify(index)}`);
    }

    const catalog = await waitForJSON(`${baseURL}/api/catalog`);
    if (catalog.source?.kind !== "store" || catalog.source?.id !== "smoke" || catalog.workflows?.length !== 1 || catalog.services?.length !== workflowStepCount) {
      throw new Error(`unexpected MySQL catalog payload: ${JSON.stringify(catalog.source || catalog)}`);
    }

    const workflows = await waitForJSON(`${baseURL}/api/workflows?filter=workflow.alpha`);
    if (!workflows.ok || workflows.source?.kind !== "store" || workflows.count !== 1 || workflows.items?.[0]?.stepCount !== workflowStepCount) {
      throw new Error(`unexpected MySQL workflow discovery payload: ${JSON.stringify(workflows)}`);
    }

    const createdBatch = await postJSON(`${baseURL}/api/cases/batch-runs`, {
      requestId: "mysql-api-smoke-workflow",
      workflowId: "workflow.alpha",
      baseUrl: `http://127.0.0.1:${targetPort}`,
      evidenceDir: path.join(tempDir, "workflow-evidence"),
      timeoutSeconds: 10,
    });
    if (createdBatch.status !== "running" || createdBatch.total !== workflowStepCount || !createdBatch.reportUrl) {
      throw new Error(`unexpected MySQL workflow batch start payload: ${JSON.stringify(createdBatch)}`);
    }
    const batchReport = await waitForWorkflowBatchReport(baseURL, createdBatch.reportUrl);
    const workflowRun = await waitForJSON(`${baseURL}/api/workflow-runs/${encodeURIComponent(batchReport.batchRunId)}`);
    if (!workflowRun?.ok || workflowRun.run?.status !== "passed" || workflowRun.run?.stepCount !== workflowStepCount) {
      throw new Error(`MySQL workflow batch report did not persist as a workflow run: ${JSON.stringify(workflowRun)}`);
    }
    for (const item of batchReport.cases) {
      const evidence = await waitForJSON(`${baseURL}/api/case/evidence?caseRunId=${encodeURIComponent(item.caseRunId)}`);
      assertCaseEvidencePayload(evidence, {
        runID: item.runId,
        caseID: item.caseId,
        stepID: item.stepId,
        path: item.path,
      });
    }

    const registration = await postJSON(`${baseURL}/api/sandbox/services`, {
      id: "service.mysql-api-smoke",
      displayName: "MySQL API Smoke Service",
      kind: "http",
      servicePort: 19090,
      healthUrl: "http://127.0.0.1:19090/health",
      status: "active",
    });
    if (!registration.ok || registration.counts?.services !== workflowStepCount + 1) {
      throw new Error(`unexpected MySQL service registration payload: ${JSON.stringify(registration)}`);
    }
    const interfaceRegistration = await postJSON(`${baseURL}/api/sandbox/interfaces`, {
      id: "interface.mysql-api-smoke",
      displayName: "MySQL API Smoke Interface",
      serviceId: "service.mysql-api-smoke",
      operation: "mysql.api.smoke",
      method: "GET",
      path: "/v1/mysql-api-smoke",
      timeoutMs: 1200,
      requestTemplate: {
        id: "template.mysql-api-smoke",
        templateJson: { query: { id: "{{override:id|demo}}" } },
      },
      case: {
        id: "case.mysql-api-smoke.default",
        displayName: "MySQL API Smoke Default",
        caseType: "success",
        requiredForAdmission: true,
        expectedJson: { ok: true },
        timeoutSeconds: 5,
      },
      caseExecution: {
        method: "GET",
        path: "/v1/mysql-api-smoke",
        expectedHttpCodes: [200],
      },
    });
    if (!interfaceRegistration.ok || interfaceRegistration.interface?.id !== "interface.mysql-api-smoke" || interfaceRegistration.interface?.caseId !== "case.mysql-api-smoke.default") {
      throw new Error(`unexpected MySQL interface registration payload: ${JSON.stringify(interfaceRegistration)}`);
    }

    const updatedCatalog = await waitForJSON(`${baseURL}/api/catalog`);
    if (!updatedCatalog.services?.some((service) => service.id === "service.mysql-api-smoke")) {
      throw new Error(`MySQL API write did not persist to Store-backed catalog: ${JSON.stringify(updatedCatalog.services)}`);
    }
    const interfaceDetail = await waitForJSON(`${baseURL}/api/interface-node?id=interface.mysql-api-smoke`);
    assertRegisteredInterfaceCatalog({ catalog: updatedCatalog, detail: interfaceDetail });

    const environmentRegistration = await postJSON(`${baseURL}/api/environments`, {
      id: "env.mysql-api-smoke",
      displayName: "MySQL API Smoke Environment",
      description: "MySQL Store API smoke Environment Catalog entry",
      services: [{ id: "service.mysql-api-smoke", repo: "https://example.invalid/mysql-api-smoke.git" }],
      repos: {
        "service.mysql-api-smoke": {
          url: "https://example.invalid/mysql-api-smoke.git",
          branch: "main",
          checkout: "/tmp/mysql-api-smoke",
        },
      },
      compose: {
        composeFile: "docker-compose.yml",
        startCommand: "docker compose up -d",
      },
      healthChecks: [{ id: "mysql-api-smoke-health", url: `http://127.0.0.1:${targetPort}/health` }],
      verificationWorkflowId: "workflow.alpha",
    });
    const environmentDiscoverAll = await waitForJSON(`${baseURL}/api/environments?all=true`);
    const environmentInspect = await waitForJSON(`${baseURL}/api/environments/env.mysql-api-smoke`);
    const environmentBootstrap = await waitForJSON(`${baseURL}/api/environments/env.mysql-api-smoke/bootstrap`);
    assertEnvironmentCatalogPayload({
      registered: environmentRegistration,
      discoverAll: environmentDiscoverAll,
      inspect: environmentInspect,
      bootstrap: environmentBootstrap,
    });

    const acceptanceStart = await postJSON(`${baseURL}/api/environments/env.mysql-api-smoke/acceptance-runs`, {
      requestId: "mysql-api-smoke-acceptance",
      baseUrl: `http://127.0.0.1:${targetPort}`,
      evidenceDir: path.join(tempDir, "environment-acceptance-evidence"),
      timeoutSeconds: 10,
    });
    if (acceptanceStart.environmentId !== "env.mysql-api-smoke" || acceptanceStart.workflowId !== "workflow.alpha" || !acceptanceStart.reportUrl) {
      throw new Error(`unexpected MySQL Environment acceptance start payload: ${JSON.stringify(acceptanceStart)}`);
    }
    const acceptanceReport = await waitForEnvironmentAcceptanceReport(baseURL, acceptanceStart.reportUrl);
    const environmentAfterAcceptance = await waitForJSON(`${baseURL}/api/environments/env.mysql-api-smoke`);
    assertEnvironmentAcceptancePayload({
      report: acceptanceReport,
      inspect: environmentAfterAcceptance,
    });

    const published = await postJSON(`${baseURL}/api/environments/env.mysql-api-smoke/publish-verified`, {});
    const environmentDiscoverVerified = await waitForJSON(`${baseURL}/api/environments`);
    const environmentAfterPublish = await waitForJSON(`${baseURL}/api/environments/env.mysql-api-smoke`);
    assertEnvironmentPublishedPayload({
      published,
      discoverVerified: environmentDiscoverVerified,
      inspect: environmentAfterPublish,
    });
  } finally {
    await closeHTTPServer(traceProvider?.server);
    await closeHTTPServer(targetServer);
    await stopServer(server);
    await rm(tempDir, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error.stack || error.message);
    process.exit(1);
  });
}
