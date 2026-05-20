import assert from "node:assert/strict";
import { test } from "node:test";

import { assertCaseEvidencePayload, assertWorkflowBatchReport, requiredMySQLDSN } from "./mysql-store-api-smoke.mjs";

test("MySQL API smoke accepts the shared SQL smoke Store env", () => {
  assert.equal(
    requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    }),
    "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
  );
});

test("MySQL API smoke prefers its dedicated DSN over shared smoke Store env", () => {
  assert.equal(
    requiredMySQLDSN({
      OTSANDBOX_MYSQL_API_SMOKE_DSN: "mysql://user:secret@example.com:3306/otsandbox_api?tls=false",
      OTSANDBOX_SMOKE_STORE_DSN: "mysql://user:secret@example.com:3306/otsandbox_release?tls=false",
      OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306/otsandbox_legacy?tls=false",
    }),
    "mysql://user:secret@example.com:3306/otsandbox_api?tls=false",
  );
});

test("MySQL API smoke rejects non-MySQL shared Store env", () => {
  assert.throws(
    () => requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "postgres://user:secret@example.com:5432/otsandbox_smoke?sslmode=disable",
    }),
    /requires a mysql:\/\/ Store DSN/,
  );
});

test("MySQL API smoke validates the async 10-step workflow batch report", () => {
  assertWorkflowBatchReport({
    ok: true,
    status: "passed",
    workflowId: "workflow.alpha",
    total: 10,
    completed: 10,
    passed: 10,
    failed: 0,
    cases: Array.from({ length: 10 }, (_, index) => ({
      caseId: `case.step-${String(index + 1).padStart(2, "0")}`,
      stepId: `step-${String(index + 1).padStart(2, "0")}`,
      status: "passed",
      runId: `run-${index + 1}`,
      caseRunId: `run-${index + 1}.case`,
      elapsedMs: 12,
    })),
  });
});

test("MySQL API smoke validates workflow case Evidence payloads", () => {
  assertCaseEvidencePayload({
    ok: true,
    evidence: {
      summary: {
        run_id: "run-1",
        case_id: "case.step-01",
        step_id: "step-01",
        status: "passed",
      },
      request: {
        method: "GET",
        path: "/v1/items/step-01",
        evidence_uri: "/tmp/request.json",
      },
      response: {
        http_code: 200,
        evidence_uri: "/tmp/response.json",
      },
      assertions: {
        status: "passed",
        passed: true,
      },
    },
  }, {
    runID: "run-1",
    caseID: "case.step-01",
    stepID: "step-01",
    path: "/v1/items/step-01",
  });
});
