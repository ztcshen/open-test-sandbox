import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  assertSmokeCatalogIndex,
  prepareSmokeTraceProvider,
  requireCompleteSmokeTraceIDs,
  smokeStepIDs,
  smokeTraceID,
  smokeWorkflowStepCount,
  writeSmokeProfile,
} from "./control-plane-smoke.mjs";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

describe("control-plane smoke Store selection", () => {
  it("prepares a named PostgreSQL Store when a smoke DSN is provided", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "const calls = [];",
        "const ref = await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_SMOKE_STORE_DSN: 'postgres://user:secret@example.com:5432/ots?sslmode=disable' }, (command, args, options) => calls.push({ command, args, env: options.env }));",
        "if (ref.storeRef !== 'smoke-postgres') throw new Error(JSON.stringify(ref));",
        "if (!ref.serverEnv.OTSANDBOX_CONFIG_HOME?.endsWith('/store-config')) throw new Error(JSON.stringify(ref));",
        "if (calls.length !== 3) throw new Error(JSON.stringify(calls));",
        "if (calls[0].args.join(' ') !== 'run ./cmd/otsandbox store config set smoke-postgres --url postgres://user:secret@example.com:5432/ots?sslmode=disable') throw new Error(JSON.stringify(calls));",
        "if (calls[1].args.join(' ') !== 'run ./cmd/otsandbox store use smoke-postgres') throw new Error(JSON.stringify(calls));",
        "if (calls[2].args.join(' ') !== 'run ./cmd/otsandbox store upgrade --store smoke-postgres') throw new Error(JSON.stringify(calls));",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
      env: { ...process.env, OTSANDBOX_SMOKE_IMPORT_ONLY: "1" },
    });
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });

  it("prepares a named MySQL Store when a smoke DSN is provided", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "const calls = [];",
        "const ref = await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_SMOKE_STORE_DSN: 'mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false' }, (command, args, options) => calls.push({ command, args, env: options.env }));",
        "if (ref.storeRef !== 'smoke-mysql') throw new Error(JSON.stringify(ref));",
        "if (calls[0].args.join(' ') !== 'run ./cmd/otsandbox store config set smoke-mysql --url mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false') throw new Error(JSON.stringify(calls));",
        "if (calls[1].args.join(' ') !== 'run ./cmd/otsandbox store use smoke-mysql') throw new Error(JSON.stringify(calls));",
        "if (calls[2].args.join(' ') !== 'run ./cmd/otsandbox store upgrade --store smoke-mysql') throw new Error(JSON.stringify(calls));",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
      env: { ...process.env, OTSANDBOX_SMOKE_IMPORT_ONLY: "1" },
    });
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });

  it("refuses likely business MySQL databases before preparing the smoke Store", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "const calls = [];",
        "await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_SMOKE_STORE_DSN: 'mysql://user:secret@example.com:3306/business_prod?tls=false' }, (command, args, options) => calls.push({ command, args, env: options.env }));",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
      env: { ...process.env, OTSANDBOX_SMOKE_IMPORT_ONLY: "1" },
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /refuses database 'business_prod'/);
    assert.doesNotMatch(result.stderr, /secret/);
  });

  it("requires a SQL DSN unless temporary SQLite smoke is explicit", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "await prepareSmokeStoreReference('/tmp/ots-smoke', {}, () => {});",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /OTSANDBOX_SMOKE_STORE_DSN or OTSANDBOX_SMOKE_STORE/);
  });

  it("can create a temporary SQLite smoke Store", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "const ref = await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE: '1' }, () => {});",
        "if (ref.storeRef !== 'sqlite:///tmp/ots-smoke/store.sqlite') throw new Error(JSON.stringify(ref));",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });

  it("accepts explicit SQLite smoke Store references", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "const calls = [];",
        "const ref = await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_SMOKE_STORE_DSN: 'sqlite:///tmp/ots-smoke/store.sqlite' }, (command, args, options) => calls.push({ command, args, env: options.env }));",
        "if (ref.storeRef !== 'smoke-sqlite') throw new Error(JSON.stringify(ref));",
        "if (!calls.some((call) => call.args.includes('upgrade'))) throw new Error(JSON.stringify(calls));",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });

  it("rejects non-SQL smoke Store references", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_SMOKE_STORE_DSN: 'redis://127.0.0.1:6379/0' }, () => {});",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /OTSANDBOX_SMOKE_STORE_DSN or OTSANDBOX_SMOKE_STORE/);
    assert.match(result.stderr, /PostgreSQL, MySQL, or SQLite Store DSN/);
  });

  it("rejects contradictory SQLite smoke flags", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { prepareSmokeStoreReference } from './tools/smoke/control-plane-smoke.mjs';",
        "await prepareSmokeStoreReference('/tmp/ots-smoke', { OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE: '1', OTSANDBOX_DISABLE_SQLITE_STORE: '1' }, () => {});",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /cannot be combined/);
  });

  it("uses a configured real SkyWalking GraphQL URL without starting the synthetic provider", async () => {
    const provider = await prepareSmokeTraceProvider({ OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql" });
    assert.equal(provider.graphQLURL, "http://skywalking.example/graphql");
    assert.equal(provider.mode, "real");
    assert.equal(provider.server, null);
  });

  it("rejects required real SkyWalking mode without a GraphQL URL", async () => {
    await assert.rejects(
      prepareSmokeTraceProvider({ OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1" }),
      /requires OTS_TRACE_GRAPHQL_URL/,
    );
  });

  it("rejects required real SkyWalking mode without trace ids for every step", async () => {
    await assert.rejects(
      prepareSmokeTraceProvider({
        OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
        OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
        OTS_SMOKE_TRACE_IDS: "step-01=trace.real.01",
      }),
      /every configured workflow step.*step-02/,
    );
  });

  it("accepts required real SkyWalking mode with all configured trace ids", async () => {
    const traceIDs = smokeStepIDs.map((stepID) => `${stepID}=trace.real.${stepID.replace("step-", "")}`).join(",");
    requireCompleteSmokeTraceIDs({ OTS_SMOKE_TRACE_IDS: traceIDs });
    const provider = await prepareSmokeTraceProvider({
      OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
      OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
      OTS_SMOKE_TRACE_IDS: traceIDs,
    });
    assert.equal(provider.graphQLURL, "http://skywalking.example/graphql");
    assert.equal(provider.mode, "real");
    assert.equal(provider.server, null);
  });

  it("accepts per-step real trace id overrides for external SkyWalking smoke", () => {
    assert.equal(smokeTraceID("step-01", "trace.smoke.01", { OTS_SMOKE_TRACE_IDS: '{"step-01":"trace.real.01"}' }), "trace.real.01");
    assert.equal(smokeTraceID("step-02", "trace.smoke.02", { OTS_SMOKE_TRACE_IDS: "step-02=trace.real.02" }), "trace.real.02");
  });
});

describe("control-plane smoke Evidence assertions", () => {
  it("requires Store-backed request, response, assertion, and topology evidence for the workflow run case", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { assertWorkflowCaseEvidence } from './tools/smoke/control-plane-smoke.mjs';",
        "assertWorkflowCaseEvidence({ ok: true, evidence: { summary: { case_id: 'case.alpha', case_run_id: 'run.case', run_id: 'run.workflow', step_id: 'step.alpha', status: 'passed' }, request: { method: 'GET', path: '/v1/items', evidence_uri: '/e/request.json' }, response: { http_code: 200, evidence_uri: '/e/response.json' }, assertions: { status: 'passed', passed: true }, topology: { provider: 'skywalking', status: 'complete', traceId: 'trace.smoke.1', observedNodes: ['service.alpha', 'service.worker'], confirmedEdges: [{ source: 'service.alpha', target: 'service.worker' }] } } }, { runID: 'run.workflow', caseID: 'case.alpha', stepID: 'step.alpha' });",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });

  it("rejects empty SkyWalking topology edges in workflow run case evidence", () => {
    const result = spawnSync(process.execPath, [
      "--input-type=module",
      "-e",
      [
        "import { assertWorkflowCaseEvidence } from './tools/smoke/control-plane-smoke.mjs';",
        "assertWorkflowCaseEvidence({ ok: true, evidence: { summary: { case_id: 'case.alpha', case_run_id: 'run.case', run_id: 'run.workflow', step_id: 'step.alpha', status: 'passed' }, request: { method: 'GET', path: '/v1/items', evidence_uri: '/e/request.json' }, response: { http_code: 200, evidence_uri: '/e/response.json' }, assertions: { status: 'passed', passed: true }, topology: { provider: 'skywalking', status: 'complete', traceId: 'trace.smoke.1', observedNodes: ['service.alpha', 'service.worker'], confirmedEdges: [{}] } } }, { runID: 'run.workflow', caseID: 'case.alpha', stepID: 'step.alpha' });",
      ].join("\n"),
    ], {
      cwd: rootDir,
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /complete SkyWalking topology evidence/);
  });
});

describe("control-plane smoke workflow shape", () => {
  it("accepts backend-specific catalog template counts with the same core assets", () => {
    const baseCounts = {
      services: smokeWorkflowStepCount,
      workflows: 1,
      interfaceNodes: smokeWorkflowStepCount,
      apiCases: smokeWorkflowStepCount,
      requestTemplates: smokeWorkflowStepCount,
      workflowBindings: smokeWorkflowStepCount,
      caseDependencies: smokeWorkflowStepCount,
      fixtures: 1,
    };
    assertSmokeCatalogIndex({
      profileId: "smoke",
      counts: { ...baseCounts, templates: 6, templateConfigs: 8 },
    });
    assertSmokeCatalogIndex({
      profileId: "smoke",
      counts: { ...baseCounts, templates: 8, templateConfigs: 8 },
    });
  });

  it("models the core button workflow as configured Store-backed steps", async () => {
    const tempDir = await mkdtemp(path.join(os.tmpdir(), "ots-smoke-profile-"));
    try {
      const profileDir = await writeSmokeProfile(tempDir, 18080);
      const raw = await readFile(path.join(profileDir, "profile.json"), "utf8");
      const profile = JSON.parse(raw);
      assert.equal(profile.workflows.length, 1);
      assert.equal(profile.services.length, smokeWorkflowStepCount);
      assert.equal(new Set(profile.services.map((item) => item.id)).size, smokeWorkflowStepCount);
      assert.equal(profile.workflowBindings.length, smokeWorkflowStepCount);
      assert.equal(profile.apiCases.length, smokeWorkflowStepCount);
      assert.equal(profile.templateConfigs.filter((item) => item.templateId === "case-execution").length, smokeWorkflowStepCount);
      assert.deepEqual(profile.workflowBindings.map((item) => item.stepId), smokeStepIDs);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});
