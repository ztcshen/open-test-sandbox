function expectedStepCount(env = process.env, configured) {
  const raw = String(env.AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS || "").trim();
  if (!raw) {
    if (configured) return configured;
    throw new Error("AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS must be set to the configured workflow step count.");
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error("AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS must be a positive integer.");
  }
  return parsed;
}

export function parseTraceIDs(value) {
  const raw = String(value || "").trim();
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return Object.fromEntries(Object.entries(parsed).map(([key, traceID]) => [key, String(traceID).trim()]));
    }
  } catch {
    // Accept comma-separated step=trace mappings when JSON is inconvenient in shell.
  }
  return Object.fromEntries(raw.split(",").map((item) => item.split("=").map((part) => part.trim())).filter(([key, traceID]) => key && traceID));
}

export function isHTTPGraphQLURL(value) {
  const raw = String(value || "").trim();
  try {
    const parsed = new URL(raw);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

export function requireSkyWalkingReleaseInputs(env = process.env, {
  label = "release-check",
  expectedSteps,
} = {}) {
  if (!String(env.AGENT_TESTBENCH_TRACE_GRAPHQL_URL || "").trim()) {
    throw new Error(`${label} requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL.`);
  }
  if (!isHTTPGraphQLURL(env.AGENT_TESTBENCH_TRACE_GRAPHQL_URL)) {
    throw new Error(`${label} requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL to be an http/https URL.`);
  }
  expectedSteps = expectedStepCount(env, expectedSteps);
  if (!String(env.AGENT_TESTBENCH_SMOKE_TRACE_IDS || "").trim()) {
    throw new Error(`${label} requires AGENT_TESTBENCH_SMOKE_TRACE_IDS for every configured workflow step.`);
  }
  const traceIDs = parseTraceIDs(env.AGENT_TESTBENCH_SMOKE_TRACE_IDS);
  const missing = Array.from({ length: expectedSteps }, (_, index) => `step-${String(index + 1).padStart(2, "0")}`)
    .filter((stepID) => !traceIDs[stepID]);
  if (missing.length > 0) {
    throw new Error(`${label} requires AGENT_TESTBENCH_SMOKE_TRACE_IDS for every configured workflow step; missing: ${missing.join(" ")}.`);
  }
  return { traceIDs, expectedSteps };
}

if (process.argv[1] && import.meta.url === new URL(process.argv[1], "file:").href) {
  try {
    requireSkyWalkingReleaseInputs(process.env, { label: process.argv[2] || "release-check" });
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}
