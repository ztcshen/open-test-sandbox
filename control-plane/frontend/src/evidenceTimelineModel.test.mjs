import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { buildEvidenceArtifacts, buildEvidenceNavigation, buildEvidenceReproduction, buildEvidenceTimeline } from "./evidenceTimelineModel.mjs";

describe("buildEvidenceTimeline", () => {
  const payload = {
    step: {
      caseId: "case.create",
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
      summary: { case_id: "case.create", operation: "POST /items" },
      request: { method: "POST", path: "/items", request_id: "req-1" },
      response: { http_code: 500, request_id: "req-1" },
      assertions: { status: "failed", passed: false, http_status_ok: false, failure_reason: "unexpected status" },
      fixture: {
        status: "configured",
        applyRuns: [{ status: "applied", fixtureInstanceId: "fixture-1" }],
        dependencies: [{ id: "dependency.alpha" }],
        summary: { applyCount: 1, dependencyCount: 1 },
      },
      topology: { provider: "skywalking", status: "complete", requestId: "req-1", traceId: "trace-1", confirmedEdges: [{ source: "service.alpha", target: "service.worker" }] },
    },
  };

  it("organizes evidence sections into a trace-style timeline with facets", () => {
    const timeline = buildEvidenceTimeline(payload);

    assert.deepEqual(timeline.facets.map(({ key, count }) => ({ key, count })), [
      { key: "request", count: 1 },
      { key: "response", count: 1 },
      { key: "assertions", count: 1 },
      { key: "fixture", count: 1 },
      { key: "topology", count: 1 },
      { key: "logs", count: 1 },
    ]);
    assert.equal(timeline.items[0].id, "request");
    assert.equal(timeline.items.find((item) => item.type === "response").tone, "failed");
    assert.equal(timeline.selectedItem.id, "request");
    assert.equal(timeline.summary.total, 6);
  });

  it("filters timeline items by type and text while keeping the selected item valid", () => {
    const timeline = buildEvidenceTimeline(payload, { type: "logs", query: "response 500", selectedId: "request" });

    assert.equal(timeline.visibleItems.length, 1);
    assert.equal(timeline.visibleItems[0].id, "logs:service.alpha");
    assert.equal(timeline.selectedItem.id, "logs:service.alpha");
    assert.equal(timeline.activeFilters.type, "logs");
    assert.equal(timeline.activeFilters.query, "response 500");
  });

  it("collects explicit and summary evidence artifacts without duplicating paths", () => {
    const artifacts = buildEvidenceArtifacts({
      ...payload,
      caseDiagnostics: {
        ...payload.caseDiagnostics,
        summary: {
          ...payload.caseDiagnostics.summary,
          evidence_path: ".runtime/evidence/run.alpha",
          report_path: "/api/case/evidence?caseRun=run.alpha&caseId=case.create",
        },
        artifacts: [
          { label: "case bundle", path: "/api/case/evidence?caseRun=run.alpha&caseId=case.create", kind: "json" },
          { label: "service logs", path: ".runtime/evidence/run.alpha/service.log", kind: "log" },
        ],
      },
    });

    assert.deepEqual(artifacts.map(({ label, kind, path, href }) => ({ label, kind, path, href })), [
      { label: "case bundle", kind: "json", path: "/api/case/evidence?caseRun=run.alpha&caseId=case.create", href: "/api/case/evidence?caseRun=run.alpha&caseId=case.create" },
      { label: "service logs", kind: "log", path: ".runtime/evidence/run.alpha/service.log", href: "" },
      { label: "evidence root", kind: "directory", path: ".runtime/evidence/run.alpha", href: "" },
    ]);
  });
});

describe("buildEvidenceReproduction", () => {
  it("builds a curl reproduction command from request evidence", () => {
    const reproduction = buildEvidenceReproduction({
      caseDiagnostics: {
        summary: { case_id: "case.create", base_url: "https://api.example.test" },
        request: {
          method: "POST",
          path: "/items",
          headers: { "content-type": "application/json", authorization: "Bearer secret" },
          body: { name: "alpha" },
        },
        response: { http_code: 500 },
        assertions: { status: "failed", failure_reason: "unexpected status" },
      },
    });

    assert.equal(reproduction.available, true);
    assert.equal(reproduction.method, "POST");
    assert.equal(reproduction.url, "https://api.example.test/items");
    assert.equal(reproduction.status, "HTTP 500");
    assert.equal(reproduction.failure, "unexpected status");
    assert.equal(reproduction.command.includes("curl -i -X POST"), true);
    assert.equal(reproduction.command.includes("-H 'content-type: application/json'"), true);
    assert.equal(reproduction.command.includes("-H 'authorization: <redacted>'"), true);
    assert.equal(reproduction.command.includes("--data '{\"name\":\"alpha\"}'"), true);
  });

  it("returns unavailable when request evidence has no method or path", () => {
    const reproduction = buildEvidenceReproduction({ caseDiagnostics: { request: {} } });

    assert.deepEqual(reproduction, { available: false, reason: "request evidence is missing method or path" });
  });
});

describe("buildEvidenceNavigation", () => {
  it("keeps workflow and case context when linking back to case runs and workflow case set", () => {
    const navigation = buildEvidenceNavigation({
      workflowId: "workflow.checkout",
      caseId: "case.create",
      caseRun: "run-1",
    });

    assert.deepEqual(navigation, {
      caseRunsHref: "/case-runs.html?case=case.create&workflow=workflow.checkout",
      workflowCaseSetHref: "/api-cases.html?workflow=workflow.checkout&case=case.create",
    });
  });

  it("falls back to the case run report when workflow context is absent", () => {
    const navigation = buildEvidenceNavigation({ caseId: "case.create" });

    assert.deepEqual(navigation, {
      caseRunsHref: "/case-runs.html?case=case.create",
      workflowCaseSetHref: "",
    });
  });
});
