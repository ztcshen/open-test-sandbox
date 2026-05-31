import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function makeRepo() {
  const repo = mkdtempSync(path.join(os.tmpdir(), "agent-testbench-secret-scan-"));
  const init = spawnSync("git", ["init"], { cwd: repo, encoding: "utf8", stdio: "pipe" });
  assert.equal(init.status, 0, init.stderr);
  return repo;
}

function runSecretScan(repo, args = []) {
  return spawnSync("bash", [path.join(rootDir, "tools", "guardrails", "check_secrets.sh"), ...args], {
    cwd: rootDir,
    env: { ...process.env, AGENT_TESTBENCH_SECRET_SCAN_ROOT: repo },
    encoding: "utf8",
    stdio: "pipe",
  });
}

test("secret scan blocks checked-in key material files", () => {
  const repo = makeRepo();
  try {
    writeFileSync(path.join(repo, "local.pem"), "not a real key\n");
    const add = spawnSync("git", ["add", "local.pem"], { cwd: repo, encoding: "utf8", stdio: "pipe" });
    assert.equal(add.status, 0, add.stderr);

    const result = runSecretScan(repo);

    assert.equal(result.status, 1);
    assert.match(`${result.stdout}\n${result.stderr}`, /Secret-like certificate or key files are not allowed/);
    assert.match(`${result.stdout}\n${result.stderr}`, /local\.pem/);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("secret scan blocks token and private-key shaped content", () => {
  const repo = makeRepo();
  try {
    const configDir = path.join(repo, "config");
    mkdirSync(configDir);
    const githubToken = "ghp_" + "1234567890abcdefghijklmnopqrstuvwxyzAB";
    const privateKeyHeader = "-----BEGIN " + "RSA PRIVATE KEY-----";
    writeFileSync(path.join(configDir, "prod.env"), [
      `ACCESS_TOKEN=${githubToken}`,
      `PRIVATE='${privateKeyHeader}'`,
      "",
    ].join("\n"));
    const add = spawnSync("git", ["add", "config/prod.env"], { cwd: repo, encoding: "utf8", stdio: "pipe" });
    assert.equal(add.status, 0, add.stderr);

    const result = runSecretScan(repo);
    const output = `${result.stdout}\n${result.stderr}`;

    assert.equal(result.status, 1);
    assert.match(output, /Secret-like token or private key content detected/);
    assert.match(output, /config\/prod\.env/);
    assert.match(output, /\[REDACTED\]/);
    assert.doesNotMatch(output, new RegExp(githubToken));
    assert.doesNotMatch(output, new RegExp("BEGIN RSA " + "PRIVATE KEY"));
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("secret scan ignores untracked scratch but scans staged scratch files", () => {
  const repo = makeRepo();
  try {
    const scratchDir = path.join(repo, ".scratch");
    mkdirSync(scratchDir);
    const untrackedToken = "ghp_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890";
    writeFileSync(path.join(scratchDir, "note.env"), `TOKEN=${untrackedToken}\n`);

    let result = runSecretScan(repo);
    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, new RegExp(untrackedToken));

    writeFileSync(path.join(scratchDir, "prod.pem"), "not a real key\n");
    const add = spawnSync("git", ["add", ".scratch/prod.pem"], { cwd: repo, encoding: "utf8", stdio: "pipe" });
    assert.equal(add.status, 0, add.stderr);

    result = runSecretScan(repo);
    const output = `${result.stdout}\n${result.stderr}`;
    assert.equal(result.status, 1);
    assert.match(output, /Secret-like certificate or key files are not allowed/);
    assert.match(output, /\.scratch\/prod\.pem/);
    assert.doesNotMatch(output, new RegExp(untrackedToken));
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("secret scan allows documented placeholders", () => {
  const repo = makeRepo();
  try {
    writeFileSync(path.join(repo, "README.md"), "Use ${SECRET_KEY} or <token> placeholders in examples.\n");
    const add = spawnSync("git", ["add", "README.md"], { cwd: repo, encoding: "utf8", stdio: "pipe" });
    assert.equal(add.status, 0, add.stderr);

    const result = runSecretScan(repo);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.match(result.stdout, /secret scan passed/);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});
