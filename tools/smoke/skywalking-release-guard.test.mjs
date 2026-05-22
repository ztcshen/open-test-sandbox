import assert from "node:assert/strict";
import { test } from "node:test";

import { isHTTPGraphQLURL, parseTraceIDs, requireSkyWalkingReleaseInputs } from "./skywalking-release-guard.mjs";

const allStepTraceIDs = Object.fromEntries(Array.from({ length: 3 }, (_, index) => {
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
      AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "ftp://skywalking.example/graphql",
      AGENT_TESTBENCH_SMOKE_TRACE_IDS: JSON.stringify(allStepTraceIDs),
      AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    }, { label: "test gate" }),
    /test gate requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL to be an http\/https URL/,
  );
});

test("SkyWalking release guard requires an explicit configured workflow step count", () => {
  assert.throws(
    () => requireSkyWalkingReleaseInputs({
      AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
      AGENT_TESTBENCH_SMOKE_TRACE_IDS: JSON.stringify(allStepTraceIDs),
    }, { label: "test gate" }),
    /AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS must be set/,
  );
});

test("SkyWalking release guard requires trace ids for every configured workflow step", () => {
  assert.throws(
    () => requireSkyWalkingReleaseInputs({
      AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
      AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
      AGENT_TESTBENCH_SMOKE_TRACE_IDS: "step-01=trace-1",
    }, { label: "test gate" }),
    /test gate requires AGENT_TESTBENCH_SMOKE_TRACE_IDS for every configured workflow step; missing: step-02/,
  );
});

test("SkyWalking release guard accepts complete configured trace ids", () => {
  const result = requireSkyWalkingReleaseInputs({
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: JSON.stringify(allStepTraceIDs),
  }, { label: "test gate" });

  assert.equal(result.expectedSteps, 3);
  assert.equal(result.traceIDs["step-03"], "trace-step-03");
});

test("SkyWalking release guard accepts a configured workflow step count", () => {
  const result = requireSkyWalkingReleaseInputs({
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "2",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: "step-01=trace-1,step-02=trace-2",
  }, { label: "test gate" });

  assert.equal(result.expectedSteps, 2);
  assert.equal(result.traceIDs["step-02"], "trace-2");
});
