import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { writeSmokeProfile } from "./control-plane-smoke.mjs";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

export function requiredMySQLDSN(env = process.env) {
  const dsn = env.OTSANDBOX_MYSQL_API_SMOKE_DSN || env.OTSANDBOX_SMOKE_STORE_DSN || env.OTSANDBOX_SMOKE_STORE || "";
  if (!dsn.trim()) {
    throw new Error("Set OTSANDBOX_MYSQL_API_SMOKE_DSN, OTSANDBOX_SMOKE_STORE_DSN, or OTSANDBOX_SMOKE_STORE to run the MySQL Store API smoke");
  }
  if (!/^mysql:\/\//i.test(dsn)) {
    throw new Error("The MySQL Store API smoke requires a mysql:// Store DSN");
  }
  return dsn;
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

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: rootDir,
      env: { ...process.env, ...options.env },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => { stdout += chunk; });
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed with ${code}\n${stdout}\n${stderr}`));
    });
  });
}

async function runJSON(command, args, options = {}) {
  const result = await runCommand(command, args, options);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`${command} ${args.join(" ")} did not emit JSON\n${result.stdout}\n${result.stderr}\n${error.message}`);
  }
}

function buildCLI(outputPath) {
  return runCommand("go", ["build", "-o", outputPath, "./cmd/otsandbox"]);
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

async function stopServer(server) {
  if (!server) return;
  if (server.exitCode !== null || server.signalCode) return;
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

function assertNoRawSecret(payload, rawDSN) {
  const raw = JSON.stringify(payload);
  const passwordMatch = rawDSN.match(/^mysql:\/\/[^:/?#]+:([^@/?#]+)@/i);
  if (passwordMatch && raw.includes(passwordMatch[1])) {
    throw new Error(`/api/store/current leaked the raw Store password: ${raw}`);
  }
}

async function main() {
  const dsn = requiredMySQLDSN();
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "ots-mysql-api-smoke-"));
  const storeName = "api-mysql";
  let server;
  try {
    const cliBin = path.join(tempDir, "otsandbox");
    await buildCLI(cliBin);
    const env = {
      OTSANDBOX_CONFIG_HOME: path.join(tempDir, "config"),
      OTSANDBOX_DISABLE_SQLITE_STORE: "1",
    };
    await mkdir(env.OTSANDBOX_CONFIG_HOME, { recursive: true });

    await runCommand(cliBin, ["store", "config", "set", storeName, "--url", dsn], { env });
    await runCommand(cliBin, ["store", "use", storeName], { env });
    await runCommand(cliBin, ["store", "upgrade", "--store", storeName], { env });
    const currentCLI = await runJSON(cliBin, ["store", "current", "--json"], { env });
    if (currentCLI?.name !== storeName || currentCLI?.backend !== "mysql") {
      throw new Error(`CLI active Store is not the named MySQL Store: ${JSON.stringify(currentCLI)}`);
    }

    const profileDir = await writeSmokeProfile(tempDir, await freePort());
    const profileHome = path.join(tempDir, "profile-home");
    const port = await freePort();
    const baseURL = `http://127.0.0.1:${port}`;
    server = spawn(cliBin, [
      "serve",
      "--profile",
      profileDir,
      "--profile-home",
      profileHome,
      "--store",
      storeName,
      "--host",
      "127.0.0.1",
      "--port",
      String(port),
    ], {
      cwd: rootDir,
      env: { ...process.env, ...env },
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let output = "";
    server.stdout.on("data", (chunk) => { output += chunk; });
    server.stderr.on("data", (chunk) => { output += chunk; });

    const currentHTTP = await waitForJSON(`${baseURL}/api/store/current`);
    if (!currentHTTP?.ok || !currentHTTP.configured || currentHTTP.name !== storeName || currentHTTP.backend !== "mysql" || currentHTTP.source !== "store-config") {
      throw new Error(`unexpected /api/store/current payload: ${JSON.stringify(currentHTTP)}\n${output}`);
    }
    assertNoRawSecret(currentHTTP, dsn);

    const index = await waitForJSON(`${baseURL}/api/template-packages/catalog-index`);
    if (index.profileId !== "smoke" || index.counts?.workflows !== 1 || index.counts?.apiCases !== 10) {
      throw new Error(`unexpected MySQL catalog index: ${JSON.stringify(index)}`);
    }

    const catalog = await waitForJSON(`${baseURL}/api/catalog`);
    if (catalog.source?.kind !== "store" || catalog.source?.id !== "smoke" || catalog.workflows?.length !== 1 || catalog.services?.length !== 10) {
      throw new Error(`unexpected MySQL catalog payload: ${JSON.stringify(catalog.source || catalog)}`);
    }

    const workflows = await waitForJSON(`${baseURL}/api/workflows?filter=workflow.alpha`);
    if (!workflows.ok || workflows.source?.kind !== "store" || workflows.count !== 1 || workflows.items?.[0]?.stepCount !== 10) {
      throw new Error(`unexpected MySQL workflow discovery payload: ${JSON.stringify(workflows)}`);
    }

    const registration = await postJSON(`${baseURL}/api/sandbox/services`, {
      id: "service.mysql-api-smoke",
      displayName: "MySQL API Smoke Service",
      kind: "http",
      servicePort: 19090,
      healthUrl: "http://127.0.0.1:19090/health",
      status: "active",
    });
    if (!registration.ok || registration.counts?.services !== 11) {
      throw new Error(`unexpected MySQL service registration payload: ${JSON.stringify(registration)}`);
    }

    const updatedCatalog = await waitForJSON(`${baseURL}/api/catalog`);
    if (!updatedCatalog.services?.some((service) => service.id === "service.mysql-api-smoke")) {
      throw new Error(`MySQL API write did not persist to Store-backed catalog: ${JSON.stringify(updatedCatalog.services)}`);
    }
  } finally {
    await stopServer(server);
    await rm(tempDir, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error.stack || error.message);
    process.exit(1);
  });
}
