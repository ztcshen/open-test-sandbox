import assert from "node:assert/strict";
import { test } from "node:test";

import { isHTTPGraphQLURL, parseTraceIDs, requireSkyWalkingReleaseInputs } from "./skywalking-release-guard.mjs";

const allStepTraceIDs = Object.fromEntries(Array.from({ length: 10 }, (_, index) => {
  const step = `step-${String(index + 1).padStart(2, "0")}`;
  return [step, `trace-${step}`];
}));

test("SkyWalking release guard accepts http and https GraphQL URLs", () => {
  assert.equal(isHTTPGraphQLURL("http://skywalking.example/graphql"), true);
  assert.equal(isHTTPGraphQLURL("https://skywalking.example/graphql"), true);
  assert.equal(isHTTPGraphQLURL("ftp://skywalking.example/graphql"), false);
  assert.equal(isHTTPGraphQLURL("not-a-url"), false);
});

test("SkyWalking release guard parses JSON and shell trace id mappings", () => {
  assert.deepEqual(parseTraceIDs(JSON.stringify({ "step-01": " trace-1 " })), { "step-01": "trace-1" });
  assert.deepEqual(parseTraceIDs("step-01=trace-1, step-02=trace-2"), {
    "step-01": "trace-1",
    "step-02": "trace-2",
  });
});

test("SkyWalking release guard requires an http or https URL", () => {
  assert.throws(
    () => requireSkyWalkingReleaseInputs({
      OTS_TRACE_GRAPHQL_URL: "ftp://skywalking.example/graphql",
      OTS_SMOKE_TRACE_IDS: JSON.stringify(allStepTraceIDs),
    }, { label: "test gate" }),
    /test gate requires OTS_TRACE_GRAPHQL_URL to be an http\/https URL/,
  );
});

test("SkyWalking release guard requires trace ids for every workflow step", () => {
  assert.throws(
    () => requireSkyWalkingReleaseInputs({
      OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
      OTS_SMOKE_TRACE_IDS: "step-01=trace-1",
    }, { label: "test gate" }),
    /test gate requires OTS_SMOKE_TRACE_IDS for all 10 workflow steps; missing: step-02/,
  );
});

test("SkyWalking release guard accepts complete 10-step trace ids", () => {
  const result = requireSkyWalkingReleaseInputs({
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_TRACE_IDS: JSON.stringify(allStepTraceIDs),
  }, { label: "test gate" });

  assert.equal(result.expectedSteps, 10);
  assert.equal(result.traceIDs["step-10"], "trace-step-10");
});
