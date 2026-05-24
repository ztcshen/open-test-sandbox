import { readFile } from "node:fs/promises";
import http from "node:http";
import { fileURLToPath } from "node:url";
import path from "node:path";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));
const catalogPath = path.join(rootDir, "examples/demo-services/catalog.json");

function parsePort(argv) {
  const index = argv.indexOf("--port");
  if (index >= 0 && argv[index + 1]) {
    return Number(argv[index + 1]);
  }
  return Number(process.env.AGENT_TESTBENCH_DEMO_SERVICE_PORT || 49190);
}

async function readBody(request) {
  let body = "";
  request.setEncoding("utf8");
  for await (const chunk of request) {
    body += chunk;
  }
  return body ? JSON.parse(body) : {};
}

function json(response, status, payload) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(payload, null, 2));
}

function retailResponse(input) {
  return {
    status: "accepted",
    workflow: "workflow.retail.fulfillment",
    orderId: input.orderId || "order-demo-001",
    steps: [
      { id: "catalog", service: "catalog-api", status: "passed" },
      { id: "payment", service: "payment-gateway", status: "passed", latencyMs: 42 },
      { id: "risk", service: "risk-rules", status: "passed", decision: "review-not-required" },
      { id: "warehouse", service: "warehouse-orchestrator", status: "passed", location: "zone-a" },
      { id: "carrier", service: "carrier-adapter", status: "passed", bookingId: "ship-demo-001" }
    ],
    evidence: ["request", "response", "timing", "allocation", "carrier-booking"]
  };
}

function iotResponse(input) {
  const temperature = Number(input.temperatureCelsius ?? 83.2);
  return {
    status: temperature > 80 ? "command-dispatched" : "observed",
    workflow: "workflow.iot.telemetry-control",
    deviceId: input.deviceId || "device-demo-17",
    anomalyScore: temperature > 80 ? 0.91 : 0.18,
    command: temperature > 80 ? { type: "reduce-duty-cycle", target: "cooling-loop" } : null,
    evidence: ["raw-telemetry", "normalized-event", "score", "command", "audit"]
  };
}

function moderationResponse(input) {
  const text = String(input.text || "demo content");
  return {
    status: text.length > 120 ? "queued-for-review" : "approved",
    workflow: "workflow.content.moderation",
    submissionId: input.submissionId || "post-demo-204",
    policy: { checked: ["harassment", "privacy", "spam"], result: "reviewable" },
    model: { confidence: 0.87, reasonCodes: ["context-needed"] },
    evidence: ["submission", "policy-trace", "model-score", "review-queue"]
  };
}

async function main() {
  const catalog = JSON.parse(await readFile(catalogPath, "utf8"));
  const port = parsePort(process.argv.slice(2));

  const server = http.createServer(async (request, response) => {
    try {
      if (request.method === "GET" && request.url === "/health") {
        json(response, 200, { status: "ok", service: "agent-testbench-demo-services" });
        return;
      }
      if (request.method === "GET" && request.url === "/scenarios") {
        json(response, 200, catalog);
        return;
      }
      if (request.method === "POST" && request.url === "/retail/orders") {
        json(response, 201, retailResponse(await readBody(request)));
        return;
      }
      if (request.method === "POST" && request.url === "/iot/telemetry") {
        json(response, 202, iotResponse(await readBody(request)));
        return;
      }
      if (request.method === "POST" && request.url === "/moderation/reviews") {
        json(response, 202, moderationResponse(await readBody(request)));
        return;
      }
      json(response, 404, { error: "not found" });
    } catch (error) {
      json(response, 400, { error: error.message });
    }
  });

  server.listen(port, "127.0.0.1", () => {
    console.log(`AgentTestBench demo services: http://127.0.0.1:${port}`);
    console.log("Scenarios: /scenarios");
  });
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
