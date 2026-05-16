import { chromium } from "playwright";

function argValue(name, fallback = "") {
  const prefix = `--${name}=`;
  const found = process.argv.find((item) => item.startsWith(prefix));
  return found ? found.slice(prefix.length) : fallback;
}

function requiredArg(name) {
  const value = argValue(name);
  if (!value) throw new Error(`--${name} is required`);
  return value;
}

async function fetchJSON(baseURL, path) {
  const response = await fetch(`${baseURL}${path}`, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    throw new Error(`${path} returned ${response.status}: ${body.error || response.statusText}`);
  }
  return body;
}

function assertWorkflowCatalog(catalog, workflowID, expectedSteps) {
  if (catalog?.source?.kind !== "read-model") {
    throw new Error(`catalog source must be read-model, got ${JSON.stringify(catalog?.source)}`);
  }
  const workflow = (catalog.workflows || []).find((item) => item.id === workflowID);
  if (!workflow) throw new Error(`workflow ${workflowID} not found`);
  if (expectedSteps > 0 && workflow.stepCount !== expectedSteps) {
    throw new Error(`workflow ${workflowID} stepCount=${workflow.stepCount}, want ${expectedSteps}`);
  }
  if (workflow.latestRun?.id && workflow.latestRun.summary) {
    throw new Error("catalog latestRun must stay lightweight and not inline full summary");
  }
  if (!workflow.latestRun?.id) {
    throw new Error(`workflow ${workflowID} missing latestRun id`);
  }
  if (workflow.latestRun.status !== "passed") {
    throw new Error(`workflow ${workflowID} latest status=${workflow.latestRun.status}`);
  }
  return workflow;
}

async function assertWorkflowRunTiming(baseURL, workflow) {
  const run = await fetchJSON(baseURL, `/api/workflow-runs/${encodeURIComponent(workflow.latestRun.id)}`);
  const steps = run?.summary?.steps || [];
  if (steps.length !== workflow.stepCount) {
    throw new Error(`cached workflow run step count=${steps.length}, want ${workflow.stepCount}`);
  }
  const timedOut = steps.filter((step) => Number(step.timeoutMs || 0) > 0 && Number(step.elapsedMs || 0) > Number(step.timeoutMs || 0));
  if (timedOut.length) {
    throw new Error(`cached workflow steps exceeded timeout: ${timedOut.map((step) => `${step.stepId}:${step.elapsedMs}/${step.timeoutMs}`).join(", ")}`);
  }
  const failed = steps.filter((step) => step.status === "failed" || step.stepOk === false || step.timeoutExceeded === true);
  if (failed.length) {
    throw new Error(`cached workflow run has failed steps: ${failed.map((step) => step.stepId).join(", ")}`);
  }
}

async function checkNoBrowserErrors(page, action) {
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await action();
  if (errors.length) throw new Error(`browser errors:\n${errors.join("\n")}`);
}

async function main() {
  const baseURL = requiredArg("base-url").replace(/\/$/, "");
  const workflowID = requiredArg("workflow");
  const expectedSteps = Number(argValue("expected-steps", "0"));

  const catalog = await fetchJSON(baseURL, "/api/catalog");
  const workflow = assertWorkflowCatalog(catalog, workflowID, expectedSteps);
  await assertWorkflowRunTiming(baseURL, workflow);

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  try {
    await checkNoBrowserErrors(page, async () => {
      const directory = await page.goto(`${baseURL}/workflows.html`, { waitUntil: "networkidle" });
      if (!directory?.ok()) throw new Error(`/workflows.html returned ${directory?.status()}`);
      await page.waitForSelector("#react-workflows-root", { timeout: 10000 });
      const links = await page.locator(`#react-workflows-root a[href*="${encodeURIComponent(workflowID)}"]`).count();
      if (links === 0) throw new Error(`workflow directory did not link to ${workflowID}`);

      const detail = await page.goto(`${baseURL}/workflow-detail.html?id=${encodeURIComponent(workflowID)}`, { waitUntil: "networkidle" });
      if (!detail?.ok()) throw new Error(`/workflow-detail.html returned ${detail?.status()}`);
      await page.waitForSelector("#react-workflow-detail-root", { timeout: 10000 });
      await page.waitForSelector(".workflow-progress-step", { timeout: 10000 });

      const progressText = await page.locator(".workflow-progress-head").innerText();
      if (!progressText.includes(`${expectedSteps || workflow.stepCount} / ${workflow.stepCount}`)) {
        throw new Error(`unexpected workflow progress text: ${JSON.stringify(progressText)}`);
      }
      if (!progressText.includes("cached run")) {
        throw new Error(`workflow detail did not render cached run: ${JSON.stringify(progressText)}`);
      }

      const stepCards = await page.locator(".workflow-progress-step").count();
      if (expectedSteps > 0 && stepCards !== expectedSteps) {
        throw new Error(`workflow detail step cards=${stepCards}, want ${expectedSteps}`);
      }
      const hrefs = await page.locator(".workflow-progress-step").evaluateAll((nodes) => nodes.map((node) => node.href));
      if (!hrefs.every((href) => href.includes(`runId=${encodeURIComponent(workflow.latestRun.id)}`))) {
        throw new Error(`not every workflow step link carries latest runId: ${JSON.stringify(hrefs.slice(0, 3))}`);
      }
      const failedCards = await page.locator(".workflow-progress-step.failed").count();
      if (failedCards > 0) throw new Error(`workflow detail rendered ${failedCards} failed step cards`);

      const coverageText = await page.locator(".workflow-coverage-panel").innerText();
      if (!coverageText.includes(`${workflow.stepCount}/${workflow.stepCount}`)) {
        throw new Error(`workflow coverage did not show full mapping: ${JSON.stringify(coverageText.slice(0, 160))}`);
      }
    });
  } finally {
    await browser.close();
  }

  console.log(`workflow pages smoke passed for ${workflowID} (${workflow.stepCount} step(s))`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
