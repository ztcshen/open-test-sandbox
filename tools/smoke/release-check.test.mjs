import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import path from "node:path";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function releaseCheckEnv(overrides = {}) {
  const env = { ...process.env };
  delete env.OTS_TRACE_GRAPHQL_URL;
  delete env.OTS_SMOKE_TRACE_IDS;
  delete env.OTSANDBOX_REQUIRE_REAL_SKYWALKING;
  return {
    ...env,
    OTSANDBOX_SMOKE_STORE_DSN: "postgres://user:pass@127.0.0.1:5432/otsandbox_smoke?sslmode=disable",
    ...overrides,
  };
}

function runReleaseCheck(env) {
  return spawnSync("bash", ["tools/release-check.sh"], {
    cwd: rootDir,
    env,
    encoding: "utf8",
    stdio: "pipe",
  });
}

test("release-check real SkyWalking mode requires a GraphQL URL before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTS_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires 10-step trace ids before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTS_SMOKE_TRACE_IDS/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires trace ids for every workflow step", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_TRACE_IDS: "step-01=trace.real.01",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /all 10 workflow steps/);
  assert.match(result.stderr, /step-02/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});
