import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { assertSkyWalkingTopologyEvidence, assertWorkflowCaseEvidence, writeSmokeProfile } from "./control-plane-smoke.mjs";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));
const cliSmokeSteps = Array.from({ length: 10 }, (_, index) => {
  const number = String(index + 1).padStart(2, "0");
  return {
    id: `step-${number}`,
    caseID: `case.step-${number}`,
    path: `/v1/items/step-${number}`,
    traceID: `trace.smoke.${number}`,
  };
});

function requiredPGStoreDSN() {
  const dsn = process.env.OTSANDBOX_CLI_STORE_DSN || process.env.OTSANDBOX_SMOKE_STORE_DSN || process.env.OTSANDBOX_SMOKE_STORE || "";
  if (!dsn.trim()) {
    throw new Error("Set OTSANDBOX_CLI_STORE_DSN or OTSANDBOX_SMOKE_STORE_DSN to run the active PostgreSQL Store CLI smoke");
  }
  if (!/^postgres(?:ql)?:\/\//i.test(dsn)) {
    throw new Error("The active Store CLI smoke requires a PostgreSQL DSN");
  }
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
    const step = cliSmokeSteps.find((item) => item.path === pathname);
    if (!step) {
      response.writeHead(404, { "content-type": "application/json" });
      response.end(JSON.stringify({ ok: false, error: "not found" }));
      return;
    }
    response.writeHead(200, {
      "content-type": "application/json",
      "request-id": `cli-smoke-request-${step.id}`,
    });
    response.end(JSON.stringify({ ok: true, id: step.id }));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

async function startTraceProvider(port) {
  const server = createServer(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    let payload = {};
    try {
      payload = JSON.parse(body || "{}");
    } catch {
      response.writeHead(400, { "content-type": "application/json" });
      response.end(JSON.stringify({ errors: [{ message: "invalid json" }] }));
      return;
    }
    const traceID = payload.variables?.traceId || "trace.smoke.01";
    const step = cliSmokeSteps.find((item) => item.traceID === traceID) || cliSmokeSteps[0];
    response.writeHead(200, { "content-type": "application/json" });
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
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

async function closeServer(server) {
  if (!server) return;
  await new Promise((resolve) => server.close(resolve));
}

function runOTS(args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn("./bin/otsandbox.sh", args, {
      cwd: rootDir,
      env: { ...process.env, ...env },
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
      reject(new Error(`otsandbox ${args.join(" ")} failed with ${code}\n${stdout}\n${stderr}`));
    });
  });
}

async function runJSON(args, env) {
  const result = await runOTS(args, env);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`otsandbox ${args.join(" ")} did not emit JSON\n${result.stdout}\n${result.stderr}\n${error.message}`);
  }
}

function assertCount(payload, key, expected, label) {
  const actual = Number(payload?.[key]);
  if (actual !== expected) {
    throw new Error(`${label} expected ${key}=${expected}, got ${actual}: ${JSON.stringify(payload)}`);
  }
}

async function main() {
  const dsn = requiredPGStoreDSN();
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "ots-cli-pg-smoke-"));
  const targetPort = await freePort();
  const tracePort = await freePort();
  let targetServer;
  let traceProvider;
  try {
    targetServer = await startTargetServer(targetPort);
    traceProvider = await startTraceProvider(tracePort);
    const profileDir = await writeSmokeProfile(tempDir, targetPort);
    const env = {
      OTSANDBOX_CONFIG_HOME: path.join(tempDir, "config"),
      OTSANDBOX_DISABLE_SQLITE_STORE: "1",
      OTS_TRACE_GRAPHQL_URL: `http://127.0.0.1:${tracePort}/graphql`,
    };
    await mkdir(env.OTSANDBOX_CONFIG_HOME, { recursive: true });

    await runOTS(["store", "config", "set", "active-pg", "--url", dsn], env);
    await runOTS(["store", "use", "active-pg"], env);
    const current = await runJSON(["store", "current", "--json"], env);
    if (current?.name !== "active-pg" || current?.backend !== "postgres") {
      throw new Error(`active Store is not PostgreSQL: ${JSON.stringify(current)}`);
    }

    await runOTS(["store", "upgrade"], env);
    const status = await runOTS(["store", "status"], env);
    if (!status.stdout.includes("Store: postgres")) {
      throw new Error(`store status did not use PostgreSQL:\n${status.stdout}`);
    }

    const publish = await runJSON(["config", "publish", "--from", profileDir, "--json"], env);
    if (publish?.profileId !== "smoke") {
      throw new Error(`unexpected publish payload: ${JSON.stringify(publish)}`);
    }

    const cases = await runJSON(["case", "discover", "--filter", "case.step", "--json"], env);
    assertCount(cases, "count", cliSmokeSteps.length, "case discover");
    const workflows = await runJSON(["workflow", "discover", "--filter", "workflow.alpha", "--json"], env);
    assertCount(workflows, "count", 1, "workflow discover");
    const plan = await runJSON(["workflow", "plan", "--workflow", "workflow.alpha", "--json"], env);
    const planSteps = Array.isArray(plan?.steps) ? plan.steps : [];
    if (planSteps.length !== cliSmokeSteps.length) {
      throw new Error(`workflow plan did not read 10 active Store steps: ${JSON.stringify(plan)}`);
    }

    const report = await runJSON([
      "workflow", "report",
      "--workflow", "workflow.alpha",
      "--base-url", `http://127.0.0.1:${targetPort}`,
      "--output-dir", path.join(tempDir, "workflow-report"),
      "--json",
    ], env);
    if (!report?.ok || report?.counts?.passed !== cliSmokeSteps.length || report?.counts?.failed !== 0 || !report?.runId) {
      throw new Error(`workflow report did not pass all steps: ${JSON.stringify(report)}`);
    }

    for (const step of cliSmokeSteps) {
      const topology = await runJSON([
        "trace", "topology", "collect",
        "--run", report.runId,
        "--step", step.id,
        "--case", step.caseID,
        "--request", `cli-smoke-request-${step.id}`,
        "--trace-id", step.traceID,
        "--json",
      ], env);
      try {
        assertSkyWalkingTopologyEvidence(topology?.traceTopology, { traceID: step.traceID });
      } catch (error) {
        throw new Error(`trace topology did not persist SkyWalking data for ${step.id}: ${JSON.stringify(topology)}`);
      }
      const evidence = await runJSON([
        "case", "evidence",
        "--run", report.runId,
        "--case-id", step.caseID,
        "--step-id", step.id,
        "--json",
      ], env);
      assertWorkflowCaseEvidence(evidence, { runID: report.runId, caseID: step.caseID, stepID: step.id, path: step.path, traceID: step.traceID });
    }

    const caseRuns = await runJSON(["case", "runs", "--run", report.runId, "--json"], env);
    if (!caseRuns?.ok || !Array.isArray(caseRuns.caseRuns) || caseRuns.caseRuns.length !== cliSmokeSteps.length) {
      throw new Error(`case runs did not read workflow results from active Store: ${JSON.stringify(caseRuns)}`);
    }
    const tasks = await runJSON(["evidence", "tasks", "--run", report.runId, "--kind", "trace_topology_collect", "--json"], env);
    if (tasks?.counts?.passed !== cliSmokeSteps.length || tasks?.counts?.failed !== 0) {
      throw new Error(`post-process tasks did not show 10 passed topology collections: ${JSON.stringify(tasks)}`);
    }
  } finally {
    await closeServer(targetServer);
    await closeServer(traceProvider);
    await rm(tempDir, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
