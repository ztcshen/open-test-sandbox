import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function freeServer() {
  return new Promise((resolve, reject) => {
    const server = http.createServer(async (request, response) => {
      const body = await readBody(request);

      if (request.method === "POST" && request.url === "/v1/items") {
        response.writeHead(201, { "content-type": "application/json" });
        response.end(JSON.stringify({ status: "created", received: body ? JSON.parse(body) : null }));
        return;
      }

      response.writeHead(404, { "content-type": "application/json" });
      response.end(JSON.stringify({ error: "not found" }));
    });

    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      resolve({ server, baseURL: `http://127.0.0.1:${address.port}` });
    });
  });
}

function readBody(request) {
  return new Promise((resolve, reject) => {
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      body += chunk;
    });
    request.on("end", () => resolve(body));
    request.on("error", reject);
  });
}

async function closeServer(server) {
  await new Promise((resolve) => server.close(resolve));
}

function run(args) {
  return new Promise((resolve, reject) => {
    const child = spawn("./bin/otsandbox.sh", args, {
      cwd: rootDir,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code !== 0) {
        reject(new Error(`${args.join(" ")} failed\n${stdout}\n${stderr}`));
        return;
      }
      resolve(stdout.trim());
    });
  });
}

function isPostgreSQLStore(reference) {
  return /^postgres(?:ql)?:\/\//i.test(String(reference || ""));
}

function isSQLiteStore(reference) {
  return /^(sqlite:\/\/|file:)/i.test(String(reference || "")) || /\.sqlite3?$/i.test(String(reference || ""));
}

function flagEnabled(value) {
  return /^(1|true|yes|on)$/i.test(String(value || ""));
}

export function demoStore(tempDir, env = process.env) {
  const explicitStore = env.OTSANDBOX_DEMO_STORE || env.OTSANDBOX_SMOKE_STORE_DSN || env.OTSANDBOX_SMOKE_STORE || "";
  const sqliteCompat = flagEnabled(env.OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO);
  if (explicitStore.trim()) {
    if (isSQLiteStore(explicitStore) && !sqliteCompat) {
      throw new Error("SQLite demo Store requires OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO=1; use OTSANDBOX_DEMO_STORE=postgres://... for the product path");
    }
    return { label: explicitStore, storeArgs: ["--store", explicitStore], upgradeArgs: ["--store", explicitStore] };
  }
  if (sqliteCompat) {
    if (flagEnabled(env.OTSANDBOX_DISABLE_SQLITE_STORE)) {
      throw new Error("OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO cannot be combined with OTSANDBOX_DISABLE_SQLITE_STORE");
    }
    const storeRef = `sqlite://${path.join(tempDir, "store.sqlite")}`;
    return { label: storeRef, storeArgs: ["--store", storeRef], upgradeArgs: ["--store", storeRef], sqliteCompat: true };
  }
  return { label: "active Store", storeArgs: [], upgradeArgs: [] };
}

async function main() {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "otsandbox-api-case-demo-"));
  const { server, baseURL } = await freeServer();

  try {
    const evidenceDir = path.join(tempDir, "evidence");
    const store = demoStore(tempDir);
    if (!store.sqliteCompat) {
      await run(["store", "upgrade", ...store.upgradeArgs]);
    }
    const output = await run([
      "case",
      "run",
      "--case",
      "examples/api-cases/create-item.json",
      "--base-url",
      baseURL,
      "--run-id",
      "demo-create-item",
      "--evidence-dir",
      evidenceDir,
      ...store.storeArgs,
    ]);

    console.log(output);
    console.log(`Demo endpoint: ${baseURL}`);
    console.log(`Evidence bundle: ${path.join(evidenceDir, "demo-create-item")}`);
    console.log(`Store: ${store.label}`);
  } finally {
    await closeServer(server);
    if (process.env.OTSANDBOX_CLEAN_DEMO_OUTPUT === "1") {
      await rm(tempDir, { recursive: true, force: true });
    } else {
      console.log(`Demo output root: ${tempDir}`);
      console.log("Set OTSANDBOX_CLEAN_DEMO_OUTPUT=1 to remove demo output automatically.");
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(error);
    process.exit(1);
  });
}
