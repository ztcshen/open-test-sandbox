import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import net from "node:net";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import path from "node:path";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));
const probeScript = path.join(rootDir, "tools/smoke/mysql-handshake-probe.py");

function runProbe(port) {
  return new Promise((resolve) => {
    const child = spawn("python3", [probeScript, "--host", "127.0.0.1", "--port", String(port), "--timeout", "2", "--json"], {
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
    child.on("close", (status) => {
      resolve({ status, stdout, stderr });
    });
  });
}

async function withServer(handler, fn) {
  const server = net.createServer(handler);
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  try {
    return await fn(server.address().port);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

function mysqlHandshakePacket() {
  const payload = Buffer.concat([
    Buffer.from([10]),
    Buffer.from("8.0.36-agent-testbench-test\0", "utf8"),
    Buffer.from([1, 0, 0, 0]),
  ]);
  const header = Buffer.from([
    payload.length & 0xff,
    (payload.length >> 8) & 0xff,
    (payload.length >> 16) & 0xff,
    0,
  ]);
  return Buffer.concat([header, payload]);
}

test("mysql handshake probe accepts a real initial handshake packet", async () => {
  await withServer((socket) => {
    socket.end(mysqlHandshakePacket());
  }, async (port) => {
    const result = await runProbe(port);
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const report = JSON.parse(result.stdout);
    assert.equal(report.ok, true);
    assert.equal(report.handshake.protocol, 10);
    assert.equal(report.handshake.serverVersion, "8.0.36-agent-testbench-test");
  });
});

test("mysql handshake probe rejects a zero-byte close before provisioning", async () => {
  await withServer((socket) => {
    socket.destroy();
  }, async (port) => {
    const result = await runProbe(port);
    assert.equal(result.status, 2);
    const report = JSON.parse(result.stdout);
    assert.equal(report.ok, false);
    assert.match(report.error, /connection closed after 0 of 4 bytes/);
  });
});
