import { spawn, spawnSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function run(command, args) {
  const result = spawnSync(command, args, { cwd: rootDir, encoding: "utf8", stdio: "pipe" });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed\n${result.stdout}\n${result.stderr}`);
  }
}

async function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

async function waitForJSON(url, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { headers: { Accept: "application/json" } });
      if (response.ok) return response.json();
      lastError = new Error(`${url} returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw lastError || new Error(`timed out waiting for ${url}`);
}

async function postJSON(url, body) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(`${url} returned ${response.status}: ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function writeSmokeProfile(baseDir) {
  const profileDir = path.join(baseDir, "profile");
  await mkdir(profileDir, { recursive: true });
  const profile = {
    id: "smoke",
    displayName: "Smoke Profile",
    description: "Generic profile for local browser smoke checks.",
    services: [{ id: "service.alpha", displayName: "Service Alpha", kind: "http" }],
    workflows: [{ id: "workflow.alpha", displayName: "Workflow Alpha", description: "Checks a generic item flow." }],
    interfaceNodes: [{ id: "node.alpha", displayName: "Node Alpha", serviceId: "service.alpha" }],
    apiCases: [{ id: "case.alpha", displayName: "Case Alpha", nodeId: "node.alpha" }],
    requestTemplates: [
      {
        id: "template.alpha",
        displayName: "Template Alpha",
        nodeId: "node.alpha",
        method: "GET",
        path: "/v1/items",
        templateJson: JSON.stringify({ method: "GET", path: "/v1/items" }),
      },
    ],
    caseDependencies: [{ id: "dependency.alpha", caseId: "case.alpha", fixtureId: "fixture.alpha", mappingsJson: "[]" }],
    workflowBindings: [{ workflowId: "workflow.alpha", stepId: "step.alpha", nodeId: "node.alpha", caseId: "case.alpha", required: true }],
    fixtures: [{ id: "fixture.alpha", displayName: "Fixture Alpha", kind: "json", dataJson: "{}" }],
  };
  await writeFile(path.join(profileDir, "profile.json"), JSON.stringify(profile, null, 2));
  return profileDir;
}

async function checkPage(browser, baseURL, pageSpec) {
  const page = await browser.newPage();
  const errors = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));

  try {
    const response = await page.goto(baseURL + pageSpec.path, { waitUntil: "networkidle" });
    if (!response?.ok()) {
      throw new Error(`${pageSpec.path} returned ${response?.status()}`);
    }
    await page.waitForSelector(pageSpec.root);
    const text = (await page.locator(pageSpec.root).innerText()).trim();
    if (text.length < 20) {
      throw new Error(`${pageSpec.path} rendered too little text: ${JSON.stringify(text)}`);
    }
    if (errors.length > 0) {
      throw new Error(`${pageSpec.path} browser errors:\n${errors.join("\n")}`);
    }
  } finally {
    await page.close();
  }
}

async function stopServer(server) {
  if (server.exitCode !== null || server.signalCode !== null) return;
  const closed = new Promise((resolve) => server.once("close", resolve));
  try {
    process.kill(-server.pid, "SIGTERM");
  } catch {
    server.kill("SIGTERM");
  }
  const stopped = await Promise.race([
    closed.then(() => true),
    new Promise((resolve) => setTimeout(() => resolve(false), 3000)),
  ]);
  if (!stopped) {
    try {
      process.kill(-server.pid, "SIGKILL");
    } catch {
      server.kill("SIGKILL");
    }
    await Promise.race([
      closed,
      new Promise((resolve) => setTimeout(resolve, 3000)),
    ]);
  }
}

async function main() {
  run("node", ["control-plane/frontend/build.mjs"]);

  const tempDir = await mkdtemp(path.join(os.tmpdir(), "otsandbox-smoke-"));
  const profileDir = await writeSmokeProfile(tempDir);
  const storePath = path.join(tempDir, "store.sqlite");
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const server = spawn("go", ["run", "./cmd/otsandbox", "serve", "--profile", profileDir, "--store-url", storePath, "--host", "127.0.0.1", "--port", String(port)], {
    cwd: rootDir,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });

  let output = "";
  server.stdout.on("data", (chunk) => {
    output += chunk;
  });
  server.stderr.on("data", (chunk) => {
    output += chunk;
  });

  try {
    const profile = await waitForJSON(`${baseURL}/api/profile`);
    if (profile.id !== "smoke") throw new Error(`unexpected profile payload: ${JSON.stringify(profile)}`);

    const imported = await postJSON(`${baseURL}/api/profile/import`, { path: profileDir });
    if (imported.profileId !== "smoke") throw new Error(`unexpected import payload: ${JSON.stringify(imported)}`);

    const index = await waitForJSON(`${baseURL}/api/profile/catalog-index`);
    if (index.profileId !== "smoke" || index.counts.workflows !== 1 || index.counts.templates !== 2) {
      throw new Error(`unexpected catalog index: ${JSON.stringify(index)}`);
    }

    const browser = await chromium.launch({ headless: true });
    try {
      const pages = [
        { path: "/index.html", root: "#react-sandbox-workbench-root" },
        { path: "/dashboard.html", root: "#react-dashboard-root" },
        { path: "/workflows.html", root: "#react-workflows-root" },
        { path: "/workflow-detail.html?id=workflow.alpha", root: "#react-workflow-detail-root" },
        { path: "/workflow-blueprint-demo.html?workflow=workflow.alpha", root: "#react-workflow-blueprint-demo-root" },
        { path: "/workflow-blueprint-new.html", root: "#react-workflow-blueprint-demo-root" },
        { path: "/api-cases.html", root: "#react-api-cases-root" },
        { path: "/interface-nodes.html", root: "#react-interface-nodes-root" },
        { path: "/agent-test.html", root: "#react-agent-test-root" },
      ];
      for (const page of pages) {
        await checkPage(browser, baseURL, page);
      }
    } finally {
      await browser.close();
    }
    console.log(`control-plane smoke passed on ${baseURL}`);
  } finally {
    await stopServer(server);
    await rm(tempDir, { recursive: true, force: true });
    if (server.exitCode !== 0 && server.exitCode !== null && !output.includes("Server closed")) {
      process.stderr.write(output);
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
