import assert from "node:assert/strict";
import { test } from "node:test";

import { assertCaseEvidencePayload, assertEnvironmentAcceptancePayload, assertEnvironmentCatalogPayload, assertEnvironmentPublishedPayload, assertRegisteredInterfaceCatalog, assertWorkflowBatchReport, requiredMySQLDSN } from "./mysql-store-api-smoke.mjs";

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

test("MySQL API smoke rejects MySQL DSNs without a database", () => {
  assert.throws(
    () => requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306?tls=false",
    }),
    /requires a mysql:\/\/ Store DSN with a database path/,
  );
});

test("MySQL API smoke refuses likely business databases", () => {
  assert.throws(
    () => requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306/business_prod?tls=false",
    }),
    /refuses database 'business_prod'/,
  );
});

test("MySQL API smoke validates the async configured workflow batch report", () => {
  const expectedSteps = 3;
  assertWorkflowBatchReport({
    ok: true,
    status: "passed",
    workflowId: "workflow.alpha",
    total: expectedSteps,
    completed: expectedSteps,
    passed: expectedSteps,
    failed: 0,
    cases: Array.from({ length: expectedSteps }, (_, index) => ({
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

test("MySQL API smoke validates registered interface catalog data", () => {
  assertRegisteredInterfaceCatalog({
    catalog: {
      apiCases: [
        { id: "case.mysql-api-smoke.default", nodeId: "interface.mysql-api-smoke" },
      ],
    },
    detail: {
      node: { id: "interface.mysql-api-smoke", serviceId: "service.mysql-api-smoke" },
      cases: [
        { id: "case.mysql-api-smoke.default", nodeId: "interface.mysql-api-smoke", requiredForAdmission: true },
      ],
      requestTemplates: [
        { id: "template.mysql-api-smoke", method: "GET", path: "/v1/mysql-api-smoke" },
      ],
    },
  });
});

test("MySQL API smoke validates Environment Catalog registration payloads", () => {
  assertEnvironmentCatalogPayload({
    registered: {
      ok: true,
      environment: {
        id: "env.mysql-api-smoke",
        status: "draft",
        verified: false,
        verificationWorkflowId: "workflow.alpha",
      },
    },
    discoverAll: {
      ok: true,
      count: 1,
      items: [{ id: "env.mysql-api-smoke", verificationWorkflowId: "workflow.alpha" }],
    },
    inspect: {
      ok: true,
      environment: { id: "env.mysql-api-smoke", verificationWorkflowId: "workflow.alpha" },
    },
    bootstrap: {
      ok: true,
      plan: {
        verificationWorkflow: "workflow.alpha",
        restore: { docker: { action: "docker-compose" } },
      },
    },
  });
});

test("MySQL API smoke validates Environment acceptance report payloads", () => {
  const expectedSteps = 3;
  assertEnvironmentAcceptancePayload({
    report: {
      ok: true,
      environmentId: "env.mysql-api-smoke",
      batchRunId: "batch.mysql-api-smoke",
      workflowId: "workflow.alpha",
      status: "passed",
      total: expectedSteps,
      completed: expectedSteps,
      passed: expectedSteps,
      failed: 0,
      acceptance: {
        ok: true,
        workflowId: "workflow.alpha",
        expectedSteps,
        completedSteps: expectedSteps,
        passedSteps: expectedSteps,
        failedSteps: 0,
        topologyProvider: "skywalking",
        healthSummary: { total: 1, passed: 1, failed: 0 },
        steps: Array.from({ length: expectedSteps }, (_, index) => ({
          stepId: `step-${String(index + 1).padStart(2, "0")}`,
          evidenceComplete: true,
          topologyComplete: true,
        })),
      },
    },
    inspect: {
      ok: true,
      environment: {
        id: "env.mysql-api-smoke",
        status: "verified-ready",
        lastVerificationRunId: "batch.mysql-api-smoke",
        lastVerificationStatus: "passed",
        evidenceComplete: true,
        topologyComplete: true,
      },
    },
  });
});

test("MySQL API smoke validates Environment publish-verified payloads", () => {
  assertEnvironmentPublishedPayload({
    published: {
      ok: true,
      environment: {
        id: "env.mysql-api-smoke",
        status: "verified",
        verified: true,
      },
    },
    discoverVerified: {
      ok: true,
      count: 1,
      items: [{ id: "env.mysql-api-smoke", status: "verified", verified: true }],
    },
    inspect: {
      ok: true,
      environment: {
        id: "env.mysql-api-smoke",
        status: "verified",
        verified: true,
        evidenceComplete: true,
        topologyComplete: true,
      },
    },
  });
});
