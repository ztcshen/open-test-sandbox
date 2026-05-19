import { chromium } from "playwright";

function argValue(name, defaultValue = "") {
  const prefix = `--${name}=`;
  const found = process.argv.find((item) => item.startsWith(prefix));
  return found ? found.slice(prefix.length) : defaultValue;
}

function requiredArg(name) {
  const value = argValue(name);
  if (!value) throw new Error(`--${name} is required`);
  return value;
}

async function assertStepPage(page, baseURL, workflowID, runID, stepID) {
  const params = new URLSearchParams({ workflow: workflowID, step: stepID, runId: runID });
  const url = `${baseURL}/workflow-step.html?${params.toString()}`;
  const response = await page.goto(url, { waitUntil: "networkidle" });
  if (!response?.ok()) throw new Error(`${url} returned ${response?.status()}`);

  await page.waitForSelector("#react-workflow-step-root");
  await page.locator(".workflow-step-topology-graph").waitFor();
  await page.locator(".workflow-step-request-response").waitFor();
  await page.locator(".workflow-step-logs").waitFor();

  const pageText = await page.locator("#react-workflow-step-root").innerText();
  if (pageText.includes("全步骤导航")) {
    throw new Error(`${stepID} still renders the full-step navigation block`);
  }

  const graphNodes = await page.locator(".workflow-step-topology-svg-node").count();
  if (graphNodes === 0) throw new Error(`${stepID} did not render topology nodes`);
  const stepNumber = (stepID.match(/\d+$/)?.[0] || "").padStart(2, "0");
  const expectedTraceID = stepNumber ? `trace.smoke.${stepNumber}` : "";
  for (const expected of ["complete", "service.alpha", "service.worker", expectedTraceID].filter(Boolean)) {
    if (!pageText.includes(expected)) {
      throw new Error(`${stepID} topology did not show ${expected}`);
    }
  }

  const requestText = await page.locator("[data-smoke-id='step-request']").innerText();
  if (requestText.trim().length < 20 || requestText.trim() === "{}") {
    throw new Error(`${stepID} did not render a useful request payload`);
  }

  const responseText = await page.locator("[data-smoke-id='step-response']").innerText();
  if (responseText.trim().length < 20 || responseText.trim() === "{}") {
    throw new Error(`${stepID} did not render a useful response payload`);
  }

  const logGroups = await page.locator(".workflow-step-log-system").count();
  if (logGroups === 0) throw new Error(`${stepID} did not render service log groups`);
}

async function main() {
  const baseURL = requiredArg("base-url").replace(/\/$/, "");
  const workflowID = requiredArg("workflow");
  const runID = requiredArg("run-id");
  const steps = requiredArg("steps").split(",").map((item) => item.trim()).filter(Boolean);
  if (!steps.length) throw new Error("--steps must contain at least one step id");

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    for (const stepID of steps) {
      await assertStepPage(page, baseURL, workflowID, runID, stepID);
    }
    if (errors.length) throw new Error(`browser errors:\n${errors.join("\n")}`);
  } finally {
    await browser.close();
  }
  console.log(`workflow-step evidence smoke passed for ${steps.length} step(s)`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
