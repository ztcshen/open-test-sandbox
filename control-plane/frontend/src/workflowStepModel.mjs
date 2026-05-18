export function trustedTopologyFromStepRun(stepRun, stepResult) {
  if (isSkyWalkingTopology(stepResult?.traceTopology)) return stepResult.traceTopology;
  const rows = Array.isArray(stepRun?.traceTopologies) ? stepRun.traceTopologies : [];
  const row = rows.find((item) => (!stepResult?.stepId || item.stepId === stepResult.stepId) && isSkyWalkingTopologyRow(item))
    || rows.find((item) => isSkyWalkingTopologyRow(item));
  if (!row) return unavailableSkyWalkingTopology();
  const parsed = parseTopologyJSON(row.topologyJson);
  return {
    ...parsed,
    provider: "skywalking",
    status: parsed.status || row.status || "unavailable",
    requestId: parsed.requestId || row.requestId || "",
    traceId: parsed.traceId || row.traceId || "",
  };
}

function isSkyWalkingTopologyRow(row) {
  if (!row || typeof row !== "object") return false;
  if (String(row.provider || "").toLowerCase() === "skywalking") return true;
  return isSkyWalkingTopology(parseTopologyJSON(row.topologyJson));
}

function isSkyWalkingTopology(value) {
  if (!value || typeof value !== "object") return false;
  const provider = String(value.provider || value.source || "").trim().toLowerCase();
  return provider === "skywalking";
}

function parseTopologyJSON(value) {
  if (!value) return {};
  if (typeof value === "object") return value;
  try {
    return JSON.parse(value);
  } catch {
    return {};
  }
}

function unavailableSkyWalkingTopology() {
  return {
    provider: "skywalking",
    status: "unavailable",
    observedNodes: [],
    confirmedEdges: [],
    externalExits: [],
    unresolvedExits: [],
    warnings: ["SkyWalking topology was not captured for this step."],
  };
}

