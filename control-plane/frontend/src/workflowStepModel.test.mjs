import test from "node:test";
import assert from "node:assert/strict";

import { trustedTopologyFromStepRun } from "./workflowStepModel.mjs";

test("trustedTopologyFromStepRun ignores topology objects without SkyWalking provider", () => {
  const topology = trustedTopologyFromStepRun(
    {
      traceTopologies: [
        {
          stepId: "apply",
          status: "complete",
          traceId: "trace.fake",
          topologyJson: JSON.stringify({
            status: "complete",
            observedNodes: ["service.entry", "service.worker"],
            confirmedEdges: [{ source: "service.entry", target: "service.worker" }],
          }),
        },
      ],
    },
    {
      stepId: "apply",
      traceTopology: {
        status: "complete",
        traceId: "trace.inline",
        observedNodes: ["service.inline", "service.worker"],
        confirmedEdges: [{ source: "service.inline", target: "service.worker" }],
      },
    },
  );

  assert.equal(topology.status, "unavailable");
  assert.deepEqual(topology.observedNodes, []);
  assert.deepEqual(topology.confirmedEdges, []);
});

test("trustedTopologyFromStepRun returns SkyWalking topology from inline step result", () => {
  const topology = trustedTopologyFromStepRun(
    { traceTopologies: [] },
    {
      stepId: "apply",
      traceTopology: {
        provider: "skywalking",
        status: "complete",
        traceId: "trace.real",
        observedNodes: ["service.entry", "service.worker"],
        confirmedEdges: [{ source: "service.entry", target: "service.worker" }],
      },
    },
  );

  assert.equal(topology.traceId, "trace.real");
  assert.equal(topology.confirmedEdges.length, 1);
});

