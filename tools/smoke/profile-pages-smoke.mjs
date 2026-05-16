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

function csvArg(name) {
  return argValue(name)
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

async function fetchJSON(baseURL, path) {
  const response = await fetch(`${baseURL}${path}`, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    throw new Error(`${path} returned ${response.status}: ${body.error || response.statusText}`);
  }
  return body;
}

function assertInterfaceDirectory(payload, expectedCount) {
  const items = payload.items || [];
  if (expectedCount > 0 && items.length !== expectedCount) {
    throw new Error(`expected ${expectedCount} interface nodes, got ${items.length}`);
  }
  const notPassed = items.filter((item) => item.admissionStatus !== "passed");
  if (notPassed.length) {
    throw new Error(`interface nodes not passed: ${notPassed.map((item) => item.id).join(", ")}`);
  }
  const missingTiming = items.filter((item) => !(item.latestElapsedMs > 0) || !(item.totalElapsedMs > 0));
  if (missingTiming.length) {
    throw new Error(`interface nodes missing timing: ${missingTiming.map((item) => item.id).join(", ")}`);
  }
  const timedOut = items.filter((item) => Number(item.timeoutMs || 0) > 0 && Number(item.latestElapsedMs || 0) > Number(item.timeoutMs || 0));
  if (timedOut.length) {
    throw new Error(`interface nodes exceeded timeout: ${timedOut.map((item) => `${item.id}:${item.latestElapsedMs}/${item.timeoutMs}`).join(", ")}`);
  }
}

function runtimeByService(dashboard) {
  return new Map((dashboard.serviceRuntime || []).map((item) => [item.serviceId, item]));
}

function assertRuntimeIdentity(dashboard, serviceIDs) {
  const byService = runtimeByService(dashboard);
  for (const serviceID of serviceIDs) {
    const runtime = byService.get(serviceID);
    if (!runtime) throw new Error(`missing runtime for ${serviceID}`);
    if (!runtime.sourcePath) throw new Error(`${serviceID} missing sourcePath`);
    if (!runtime.branchName) throw new Error(`${serviceID} missing branchName`);
    if (!runtime.commitId) throw new Error(`${serviceID} missing commitId`);
  }
}

async function checkPage(browser, url, rootSelector, expectedText = []) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(url, { waitUntil: "networkidle" });
    if (!response?.ok()) throw new Error(`${url} returned ${response?.status()}`);
    await page.waitForSelector(rootSelector, { timeout: 10000 });
    const text = await page.locator(rootSelector).innerText();
    for (const expected of expectedText) {
      if (!text.includes(expected)) throw new Error(`${url} missing text ${JSON.stringify(expected)}`);
    }
    if (errors.length) throw new Error(`${url} browser errors:\n${errors.join("\n")}`);
  } finally {
    await page.close();
  }
}

async function main() {
  const baseURL = requiredArg("base-url").replace(/\/$/, "");
  const expectedInterfaces = Number(argValue("expected-interfaces", "0"));
  const interfaceIDs = csvArg("interfaces");
  const runtimeServices = csvArg("runtime-services");
  const environmentIDs = csvArg("environments");

  const [interfaces, dashboard] = await Promise.all([
    fetchJSON(baseURL, "/api/interface-nodes"),
    fetchJSON(baseURL, "/api/dashboard"),
  ]);
  assertInterfaceDirectory(interfaces, expectedInterfaces);
  assertRuntimeIdentity(dashboard, runtimeServices);

  const browser = await chromium.launch({ headless: true });
  try {
    for (const id of interfaceIDs) {
      await checkPage(
        browser,
        `${baseURL}/interface-node.html?id=${encodeURIComponent(id)}`,
        "#react-interface-node-root",
        ["passed"],
      );
    }
    for (const id of environmentIDs) {
      const runtime = runtimeByService(dashboard).get(id);
      await checkPage(
        browser,
        `${baseURL}/environment-node.html?id=${encodeURIComponent(id)}`,
        "#react-environment-node-root",
        [runtime.branchName, runtime.commitId],
      );
    }
  } finally {
    await browser.close();
  }

  console.log(`profile page smoke passed: ${interfaceIDs.length} interface page(s), ${environmentIDs.length} environment page(s)`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
